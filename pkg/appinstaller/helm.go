package appinstaller

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/errcode"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var (
	systemServerHost = ""
	middlewareTypes  = []string{tapr.TypePostgreSQL.String(), tapr.TypeMongoDB.String(), tapr.TypeRedis.String(), tapr.TypeNats.String()}
)

func init() {
	flag.StringVar(&systemServerHost, "system-server", "",
		"user's system-server host")
}

// HelmOpsInterface is an interface that defines operations related to helm chart.
type HelmOpsInterface interface {
	// Uninstall is the action for uninstall a release.
	Uninstall() error
	// Install is the action for install a release.
	Install() error
	// Upgrade is the action for upgrade a release.
	Upgrade() error
	// RollBack is the action for rollback a release.
	RollBack() error
	WaitForLaunch() (bool, error)

	// Install2() error
}

// Opt options for helm ops.
type Opt struct {
	Source string
}

var _ HelmOpsInterface = &HelmOps{}

// HelmOps implements HelmOpsInterface.
type HelmOps struct {
	HelmOpsInterface
	ctx          context.Context
	kubeConfig   *rest.Config
	actionConfig *action.Configuration
	app          *appcfg.ApplicationConfig
	settings     *cli.EnvSettings
	token        string
	//client       *kubernetes.Clientset
	//dyClient dynamic.Interface
	client  *clientset.ClientSet
	options Opt

	installStrategy InstallStrategy
}

func (h *HelmOps) setInstallStrategy(strategy InstallStrategy) {
	h.installStrategy = strategy
}

func (h *HelmOps) status() (*helmrelease.Release, error) {
	statusClient := action.NewStatus(h.actionConfig)
	status, err := statusClient.Run(h.app.AppName)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (h *HelmOps) userZone() (string, error) {
	return kubesphere.GetUserZone(h.ctx, h.kubeConfig, h.app.OwnerName)
}

func (h *HelmOps) registerAppPerm(perm []appcfg.SysDataPermission) (*appcfg.RegisterResp, error) {
	register := appcfg.PermissionRegister{
		App:   h.app.AppName,
		AppID: h.app.AppID,
		Perm:  perm,
	}

	url := fmt.Sprintf("http://%s/permission/v1alpha1/register", h.systemServerHost())
	client := resty.New()

	body, err := json.Marshal(register)
	if err != nil {
		return nil, err
	}

	klog.Infof("Sending app register request with body=%s url=%s", utils.PrettyJSON(string(body)), url)

	resp, err := client.SetTimeout(2*time.Second).R().
		SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
		SetHeader(constants.AuthorizationTokenKey, h.token).
		SetBody(body).Post(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != 200 {
		dump, e := httputil.DumpRequest(resp.Request.RawRequest, true)
		if e == nil {
			klog.Errorf("Failed to get response body=%s url=%s", string(dump), url)
		}

		return nil, errors.New(string(resp.Body()))
	}

	var regResp appcfg.RegisterResp
	err = json.Unmarshal(resp.Body(), &regResp)
	if err != nil {
		klog.Errorf("Failed to unmarshal response body=%s err=%v", string(resp.Body()), err)
		return nil, err
	}

	return &regResp, nil
}

func (h *HelmOps) systemServerHost() string {
	if systemServerHost != "" {
		return systemServerHost
	}

	return fmt.Sprintf("system-server.user-system-%s", h.app.OwnerName)
}

func (h *HelmOps) selectNode() (node string, appCache, userspace string, err error) {
	k8s, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return "", "", "", err
	}

	bflPods, err := k8s.CoreV1().Pods(h.ownerNamespace()).List(h.ctx,
		metav1.ListOptions{LabelSelector: "tier=bfl"})
	if err != nil {
		return "", "", "", err
	}

	if len(bflPods.Items) > 0 {
		bfl := bflPods.Items[0]

		vols := bfl.Spec.Volumes
		if len(vols) < 1 {
			return "", "", "", errors.New("user space not found")
		}

		// find user space pvc
		for _, vol := range vols {
			if vol.Name == constants.UserAppDataDirPVC || vol.Name == constants.UserSpaceDirPVC {
				if vol.PersistentVolumeClaim != nil {
					// find user space path
					pvc, err := k8s.CoreV1().PersistentVolumeClaims(h.ownerNamespace()).Get(h.ctx,
						vol.PersistentVolumeClaim.ClaimName,
						metav1.GetOptions{})
					if err != nil {
						return "", "", "", err
					}

					pv, err := k8s.CoreV1().PersistentVolumes().Get(h.ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
					if err != nil {
						return "", "", "", err
					}

					var path string
					if pv.Spec.Local != nil {
						path = pv.Spec.Local.Path
					}
					if path == "" {
						path = pv.Spec.HostPath.Path
					}

					switch vol.Name {
					case constants.UserAppDataDirPVC:
						appCache = path
					case constants.UserSpaceDirPVC:
						userspace = path
					}
				}
			}
		}

		if appCache == "" || userspace == "" {
			return "", "", "", errors.New("user space not found")
		}

		return bfl.Spec.NodeName, appCache, userspace, nil
	}

	return "", "", "", errors.New("node not found")
}

func (h *HelmOps) ownerNamespace() string {
	return utils.UserspaceName(h.app.OwnerName)
}

func (h *HelmOps) unregisterAppPerm() error {
	register := appcfg.PermissionRegister{
		App:   h.app.AppName,
		AppID: h.app.AppID,
	}

	url := fmt.Sprintf("http://%s/permission/v1alpha1/unregister", h.systemServerHost())
	client := resty.New()

	resp, err := client.SetTimeout(2*time.Second).R().
		SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
		SetHeader(constants.AuthorizationTokenKey, h.token).
		SetBody(register).Post(url)
	if err != nil {
		return err
	}

	if resp.StatusCode() != 200 {
		dump, e := httputil.DumpRequest(resp.Request.RawRequest, true)
		if e == nil {
			klog.Errorf("Failed to get response body=%s url=%s", string(dump), url)
		}

		return errors.New(string(resp.Body()))
	}

	return nil
}

// Uninstall do a uninstall operation for release.
func (h *HelmOps) Uninstall() error {
	client, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	if !apputils.IsProtectedNamespace(h.app.Namespace) {
		pvcs, err := client.CoreV1().PersistentVolumeClaims(h.app.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, pvc := range pvcs.Items {
			err = client.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	appCacheDirs, err := apputils.TryToGetAppdataDirFromDeployment(context.TODO(), h.app.Namespace, h.app.AppName, h.app.OwnerName)
	if err != nil {
		klog.Warningf("get app %s cache dir failed %v", h.app.AppName, err)
	}

	err = helm.UninstallCharts(h.actionConfig, h.app.AppName)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Errorf("failed to uninstall app %s, err=%v", h.app.AppName, err)
		return err
	}
	err = h.unregisterAppPerm()
	if err != nil {
		klog.Warningf("Failed to unregister app err=%v", err)
	}

	// delete middleware requests crd
	namespace := fmt.Sprintf("%s-%s", "user-system", h.app.OwnerName)
	for _, mt := range middlewareTypes {
		name := fmt.Sprintf("%s-%s", h.app.AppName, mt)
		err = tapr.DeleteMiddlewareRequest(context.TODO(), h.kubeConfig, namespace, name)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete middleware request namespace=%s name=%s err=%v", namespace, name, err)
		}
	}

	if len(appCacheDirs) > 0 {
		klog.Infof("clear app cache dirs: %v", appCacheDirs)
		terminusNonce, e := utils.GenTerminusNonce()
		if e != nil {
			klog.Errorf("Failed to generate terminus nonce err=%v", e)
		} else {
			c := resty.New().SetTimeout(2*time.Second).
				SetHeader(constants.AuthorizationTokenKey, h.token).
				SetHeader("Terminus-Nonce", terminusNonce)
			nodes, e := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			if e == nil {
				for _, dir := range appCacheDirs {
					for _, n := range nodes.Items {
						URL := fmt.Sprintf(constants.AppDataDirURL, h.app.OwnerName, dir)
						c.SetHeader("X-Terminus-Node", n.Name)
						c.SetHeader("x-bfl-user", h.app.OwnerName)
						res, e := c.R().Delete(URL)
						if e != nil {
							klog.Errorf("Failed to delete dir err=%v", e)
						}
						if res.StatusCode() != http.StatusOK {
							klog.Infof("delete app cache failed with: %v", res.String())
						}
					}
				}
			} else {
				klog.Errorf("Failed to get nodes err=%v", e)
			}
		}
	}

	if !apputils.IsProtectedNamespace(h.app.Namespace) {
		klog.Infof("deleting namespace %s", h.app.Namespace)
		err = client.CoreV1().Namespaces().Delete(context.TODO(), h.app.Namespace, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// Upgrade do a upgrade operation for release.
func (h *HelmOps) Upgrade() error {
	status, err := h.status()
	if err != nil {
		klog.Errorf("get release status failed %v", err)
		return err
	}
	if status.Info.Status == helmrelease.StatusDeployed {
		return h.upgrade()
	}
	return fmt.Errorf("cannot upgrade release %s/%s, current state is %s", h.app.Namespace, h.app.AppName, status.Info.Status)
}

func (h *HelmOps) upgrade() error {
	values, err := h.setValues()
	if err != nil {
		return err
	}
	namespace := fmt.Sprintf("%s-%s", "user-system", h.app.OwnerName)
	if err := tapr.Apply(h.app.Middleware, h.kubeConfig, h.app.AppName, h.app.Namespace,
		namespace, h.token, h.app.ChartsName, h.app.OwnerName, values); err != nil {
		klog.Errorf("Failed to apply middleware err=%v", err)
		return err
	}
	err = helm.UpgradeCharts(h.ctx, h.actionConfig, h.settings, h.app.AppName, h.app.ChartsName, h.app.RepoURL, h.app.Namespace, values, false)
	if err != nil {
		klog.Errorf("Failed to upgrade chart name=%s err=%v", h.app.AppName, err)
		return err
	}
	err = h.addApplicationLabelsToDeployment()
	if err != nil {
		//h.rollBack()
		return err
	}

	isDepClusterScopedApp := false
	clientset, err := versioned.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	apps, err := clientset.AppV1alpha1().Applications().List(h.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, dep := range h.app.Dependencies {
		if dep.Type == constants.DependencyTypeSystem {
			continue
		}
		for _, app := range apps.Items {
			if app.Spec.Name == dep.Name && app.Spec.Settings["clusterScoped"] == "true" {
				isDepClusterScopedApp = true
				break
			}
		}
	}

	if isDepClusterScopedApp {
		err = h.addLabelToNamespaceForDependClusterApp()
		if err != nil {
			//h.rollBack()
			return err
		}
	}
	appClient, err := versioned.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	var deployment client.Object
	if userspace.IsSysApp(h.app.AppName) {
		application, err := appClient.AppV1alpha1().Applications().Get(context.Background(),
			appv1alpha1.AppResourceName(h.app.AppName, h.app.Namespace), metav1.GetOptions{})
		if err != nil {
			return err
		}
		clientset, err := kubernetes.NewForConfig(h.kubeConfig)
		if err != nil {
			return err
		}
		deployment, err = clientset.AppsV1().Deployments(h.app.Namespace).
			Get(context.Background(), application.Spec.DeploymentName, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return err
		}
		deployment, err = clientset.AppsV1().StatefulSets(h.app.Namespace).
			Get(context.Background(), application.Spec.DeploymentName, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return err
		}
		entrancesLabel := deployment.GetAnnotations()[constants.ApplicationEntrancesKey]
		entrances, err := ToEntrances(entrancesLabel)
		if err != nil {
			return err
		}
		h.app.Entrances = entrances
	}
	for i, v := range h.app.Entrances {
		if v.AuthLevel == "" {
			h.app.Entrances[i].AuthLevel = constants.AuthorizationLevelOfPrivate
		}
	}

	var policyStr string
	if !userspace.IsSysApp(h.app.AppName) {
		if appCfg, err := appcfg.GetAppInstallationConfig(h.app.AppName, h.app.OwnerName); err != nil {
			klog.Infof("Failed to get app configuration appName=%s owner=%s err=%v", h.app.AppName, h.app.OwnerName, err)
		} else {
			policyStr, err = getApplicationPolicy(appCfg.Policies, h.app.Entrances)
			if err != nil {
				klog.Errorf("Failed to encode json err=%v", err)
			}
		}
	} else {
		// sys applications.
		type Policies struct {
			Policies []appcfg.Policy `json:"policies"`
		}
		applicationPoliciesFromAnnotation, ok := deployment.GetAnnotations()[constants.ApplicationPolicies]

		var policy Policies
		if ok {
			err := json.Unmarshal([]byte(applicationPoliciesFromAnnotation), &policy)
			if err != nil {
				klog.Errorf("Failed to unmarshal applicationPoliciesFromAnnotation err=%v", err)
			}
		}

		// transform from Policy to AppPolicy
		var appPolicies []appcfg.AppPolicy
		for _, p := range policy.Policies {
			d, _ := time.ParseDuration(p.Duration)
			appPolicies = append(appPolicies, appcfg.AppPolicy{
				EntranceName: p.EntranceName,
				URIRegex:     p.URIRegex,
				Level:        p.Level,
				OneTime:      p.OneTime,
				Duration:     d,
			})
		}
		policyStr, err = getApplicationPolicy(appPolicies, h.app.Entrances)
		if err != nil {
			klog.Errorf("Failed to encode json err=%v", err)
		}
	}
	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"entrances": h.app.Entrances,
		},
	}
	if len(policyStr) > 0 {
		patchData = map[string]interface{}{
			"spec": map[string]interface{}{
				"entrances": h.app.Entrances,
				"settings": map[string]string{
					"policy": policyStr,
				},
			},
		}
	}
	patchByte, err := json.Marshal(patchData)
	if err != nil {
		return err
	}
	name, _ := apputils.FmtAppMgrName(h.app.AppName, h.app.OwnerName, h.app.Namespace)
	_, err = appClient.AppV1alpha1().Applications().Patch(h.ctx, name, types.MergePatchType, patchByte, metav1.PatchOptions{})
	if err != nil {
		return err
	}

	ok, err := h.waitForStartUp()
	if !ok {
		// canceled
		//h.rollBack()
		return err
	}

	return nil
}

// RollBack do a rollback for release if it can be rollback.
func (h *HelmOps) RollBack() error {
	can, err := h.canRollBack()
	if err != nil {
		return err
	}
	if can {
		return h.rollBack()
	}
	return errors.New("can not do rollback")
}

func (h *HelmOps) canRollBack() (bool, error) {
	client := action.NewGet(h.actionConfig)
	release, err := client.Run(h.app.AppName)
	if err != nil {
		return false, err
	}
	if release.Version > 1 {
		return true, nil
	}
	return false, nil
}

// rollBack to previous version
func (h *HelmOps) rollBack() error {
	err := helm.RollbackCharts(h.actionConfig, h.app.AppName)
	if err != nil {
		return err
	}
	return nil
}

func (h *HelmOps) createOIDCClient(values map[string]interface{}, userZone, namespace string) error {
	client, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}

	id := h.app.AppID + "." + h.app.OwnerName
	secret := utils.GetRandomCharacters()

	values["oidc"] = map[string]interface{}{
		"client": map[string]interface{}{
			"id":     id,
			"secret": secret,
		},
		"issuer": "https://auth." + userZone,
	}

	oidcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.OIDCSecret,
			Namespace: namespace,
		},
		StringData: map[string]string{
			"id":     id,
			"secret": secret,
		},
	}
	_, err = client.CoreV1().Namespaces().Get(h.ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		ns := &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Namespace",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"name": namespace,
				},
			},
		}
		_, err = client.CoreV1().Namespaces().Create(h.ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	_, err = client.CoreV1().Secrets(namespace).Get(h.ctx, oidcSecret.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		err = client.CoreV1().Secrets(namespace).Delete(h.ctx, oidcSecret.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	_, err = client.CoreV1().Secrets(namespace).Create(h.ctx, oidcSecret, metav1.CreateOptions{})
	if err != nil {
		klog.Error("create oidc secret error, ", err)
		return err
	}

	return nil
}

func (h *HelmOps) waitForLaunch() (bool, error) {
	ok, _ := h.waitForStartUp()
	if !ok {
		return false, api.ErrStartUpFailed
	}

	timer := time.NewTicker(2 * time.Second)
	entrances := h.app.Entrances
	entranceCount := len(entrances)
	for {
		select {
		case <-timer.C:
			count := 0
			for _, e := range entrances {
				klog.Info("Waiting service for launch :", e.Host)
				host := fmt.Sprintf("%s.%s", e.Host, h.app.Namespace)
				if apputils.TryConnect(host, strconv.Itoa(int(e.Port))) {
					count++
				}
			}
			if entranceCount == count {
				return true, nil
			}

		case <-h.ctx.Done():
			klog.Infof("Waiting for launch canceled appName=%s", h.app.AppName)
			return false, api.ErrLaunchFailed
		}
	}
}
func (h *HelmOps) waitForStartUp() (bool, error) {
	timer := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-timer.C:
			startedUp, err := h.isStartUp()
			klog.Infof("wait for app %s start up", h.app.AppName)
			if startedUp {
				name, _ := apputils.FmtAppMgrName(h.app.AppName, h.app.OwnerName, h.app.Namespace)
				err := apputils.UpdateAppMgrState(h.ctx, name, appv1alpha1.Initializing)
				if err != nil {
					klog.Errorf("update appmgr state failed %v", err)
				}
				return true, nil
			}
			if errors.Is(err, errcode.ErrPodPending) {
				return false, err
			}

		case <-h.ctx.Done():
			klog.Infof("Waiting for app startup canceled appName=%s", h.app.AppName)
			return false, nil
		}
	}
}

func (h *HelmOps) isStartUp() (bool, error) {
	var labelSelector string
	deployment, err := h.client.KubeClient.Kubernetes().AppsV1().Deployments(h.app.Namespace).
		Get(h.ctx, h.app.AppName, metav1.GetOptions{})

	if err == nil {
		labelSelector = metav1.FormatLabelSelector(deployment.Spec.Selector)
	}

	if apierrors.IsNotFound(err) {
		sts, err := h.client.KubeClient.Kubernetes().AppsV1().StatefulSets(h.app.Namespace).
			Get(h.ctx, h.app.AppName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		labelSelector = metav1.FormatLabelSelector(sts.Spec.Selector)
	}
	pods, err := h.client.KubeClient.Kubernetes().CoreV1().Pods(h.app.Namespace).
		List(h.ctx, metav1.ListOptions{LabelSelector: labelSelector})

	if err != nil {
		klog.Errorf("app %s get pods err %v", h.app.AppName, err)
		return false, err
	}

	if len(pods.Items) == 0 {
		return false, errors.New("no pod found")
	}
	for _, pod := range pods.Items {
		creationTime := pod.GetCreationTimestamp()
		pendingDuration := time.Since(creationTime.Time)

		if pod.Status.Phase == corev1.PodPending && pendingDuration > time.Minute*10 {
			return false, errcode.ErrPodPending
		}
		totalContainers := len(pod.Spec.Containers)
		startedContainers := 0
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]
			if *container.Started {
				startedContainers++
			}
		}
		if startedContainers == totalContainers {
			return true, nil
		}
	}
	return false, nil
}

type applicationSettingsSubPolicy struct {
	URI      string `json:"uri"`
	Policy   string `json:"policy"`
	OneTime  bool   `json:"one_time"`
	Duration int32  `json:"valid_duration"`
}

type applicationSettingsPolicy struct {
	DefaultPolicy string                          `json:"default_policy"`
	SubPolicies   []*applicationSettingsSubPolicy `json:"sub_policies"`
	OneTime       bool                            `json:"one_time"`
	Duration      int32                           `json:"valid_duration"`
}

func getApplicationPolicy(policies []appcfg.AppPolicy, entrances []appv1alpha1.Entrance) (string, error) {
	subPolicy := make(map[string][]*applicationSettingsSubPolicy)

	for _, p := range policies {
		subPolicy[p.EntranceName] = append(subPolicy[p.EntranceName],
			&applicationSettingsSubPolicy{
				URI:      p.URIRegex,
				Policy:   p.Level,
				OneTime:  p.OneTime,
				Duration: int32(p.Duration / time.Second),
			})
	}

	policy := make(map[string]applicationSettingsPolicy)
	for _, e := range entrances {
		defaultPolicy := "system"
		sp := subPolicy[e.Name]
		if e.AuthLevel == constants.AuthorizationLevelOfPublic {
			defaultPolicy = constants.AuthorizationLevelOfPublic
		}
		policy[e.Name] = applicationSettingsPolicy{
			DefaultPolicy: defaultPolicy,
			OneTime:       false,
			Duration:      0,
			SubPolicies:   sp,
		}
	}

	policyStr, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(policyStr), nil
}

func parseAppPermission(data []appcfg.AppPermission) []appcfg.AppPermission {
	permissions := make([]appcfg.AppPermission, 0)
	for _, p := range data {
		switch perm := p.(type) {
		case string:
			if perm == "appdata-perm" {
				permissions = append(permissions, appcfg.AppDataRW)
			}
			if perm == "appcache-perm" {
				permissions = append(permissions, appcfg.AppCacheRW)
			}
			if perm == "userdata-perm" {
				permissions = append(permissions, appcfg.UserDataRW)
			}
		case appcfg.AppDataPermission:
			permissions = append(permissions, appcfg.AppDataRW)
		case appcfg.AppCachePermission:
			permissions = append(permissions, appcfg.AppCacheRW)
		case appcfg.UserDataPermission:
			permissions = append(permissions, appcfg.UserDataRW)
		case []appcfg.SysDataPermission:
			permissions = append(permissions, p)
		case []interface{}:
			var sps []appcfg.SysDataPermission
			for _, item := range perm {
				if m, ok := item.(map[string]interface{}); ok {
					var sp appcfg.SysDataPermission
					if appName, ok := m["appName"].(string); ok {
						sp.AppName = appName
					}
					if port, ok := m["port"].(string); ok {
						sp.Port = port
					}
					if svc, ok := m["svc"].(string); ok {
						sp.Svc = svc
					}
					if ns, ok := m["namespace"].(string); ok {
						sp.Namespace = ns
					}
					if group, ok := m["group"].(string); ok {
						sp.Group = group
					}
					if dataType, ok := m["dataType"].(string); ok {
						sp.DataType = dataType
					}
					if version, ok := m["version"].(string); ok {
						sp.Version = version
					}

					if ops, okk := m["ops"].([]interface{}); okk {
						sp.Ops = make([]string, len(ops))
						for i, op := range ops {
							sp.Ops[i] = op.(string)
						}
					} else {
						sp.Ops = []string{}
					}
					sps = append(sps, sp)
				}
			}
			permissions = append(permissions, sps)
		}
	}
	return permissions
}

func (h *HelmOps) Install() error {
	if h.installStrategy == nil {
		h.installStrategy = NewInstallV1(h)
	}
	return h.installStrategy.Install()
}

func (h *HelmOps) isPending() error {
	return nil
}

func (h *HelmOps) WaitForLaunch() (bool, error) {
	//req := reconcile.Request{NamespacedName: types.NamespacedName{
	//	Namespace: h.app.OwnerName,
	//}}
	//task.WQueue.(*task.Type).SetCompleted(req)
	//
	//klog.Infof("dequeue username:%s,appname:%s", h.app.OwnerName, h.app.AppName)

	timer := time.NewTicker(1 * time.Second)
	entrances := h.app.Entrances
	entranceCount := len(entrances)
	for {
		select {
		case <-timer.C:
			count := 0
			for _, e := range entrances {
				klog.Info("Waiting service for launch :", e.Host)
				host := fmt.Sprintf("%s.%s", e.Host, h.app.Namespace)
				if apputils.TryConnect(host, strconv.Itoa(int(e.Port))) {
					count++
				}
			}
			if entranceCount == count {
				return true, nil
			}

		case <-h.ctx.Done():
			klog.Infof("Waiting for launch canceled appName=%s", h.app.AppName)
			return false, h.ctx.Err()
		}
	}
}

type InstallStrategy interface {
	Install() error
}

type InstallVersion string

const (
	InstallVersionV1 InstallVersion = "v1"
	InstallVersionV2 InstallVersion = "v2"
)

func NewHelmOpsWithVersion(ctx context.Context, kubeConfig *rest.Config, app *appcfg.ApplicationConfig, token string, options Opt, version InstallVersion) (HelmOpsInterface, error) {
	actionConfig, settings, err := helm.InitConfig(kubeConfig, app.Namespace)
	if err != nil {
		return nil, err
	}

	client, err := clientset.New(kubeConfig)
	if err != nil {
		return nil, err
	}
	ops := &HelmOps{
		ctx:          ctx,
		kubeConfig:   kubeConfig,
		app:          app,
		actionConfig: actionConfig,
		settings:     settings,
		token:        token,
		client:       client,
		options:      options,
	}

	switch version {
	case InstallVersionV1:
		ops.setInstallStrategy(NewInstallV1(ops))
	case InstallVersionV2:
		ops.setInstallStrategy(NewInstallV2(ops))
	default:
		ops.setInstallStrategy(NewInstallV1(ops))
	}

	return ops, nil
}
