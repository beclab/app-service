package appinstaller

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/task"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	userspacev1 "bytetrade.io/web3os/app-service/pkg/users/userspace/v1"
	"bytetrade.io/web3os/app-service/pkg/utils"

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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
}

// Opt options for helm ops.
type Opt struct {
	Source string
}

// HelmOps implements HelmOpsInterface.
type HelmOps struct {
	HelmOpsInterface
	ctx          context.Context
	kubeConfig   *rest.Config
	actionConfig *action.Configuration
	app          *ApplicationConfig
	settings     *cli.EnvSettings
	token        string
	//client       *kubernetes.Clientset
	//dyClient dynamic.Interface
	client  *clientset.ClientSet
	options Opt
}

func (h *HelmOps) install(values map[string]interface{}) error {
	_, err := h.status()
	if err == nil {
		return driver.ErrReleaseExists
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return helm.InstallCharts(h.ctx, h.actionConfig, h.settings, h.app.AppName, h.app.ChartsName, h.app.RepoURL, h.app.Namespace, values)
	}
	return err
}

// Install makes install operation for an application.
func (h *HelmOps) Install() error {
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
	err = h.install(values)
	if err != nil && !errors.Is(err, driver.ErrReleaseExists) {
		klog.Errorf("Failed to install chart err=%v", err)
		return err
	}
	err = h.addApplicationLabelsToDeployment()
	if err != nil {
		h.Uninstall()
		return err
	}

	isDepClusterScopedApp := false
	client, err := versioned.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	apps, err := client.AppV1alpha1().Applications().List(h.ctx, metav1.ListOptions{})
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
			h.Uninstall()
			return err
		}
	}
	ok, err := h.waitForLaunch()

	if !ok {
		// install operation has been canceled, so to uninstall it.
		h.Uninstall()
		//return context.Canceled
		return err
	}
	klog.Infof("app: %s launched success", h.app.AppName)

	return nil
}

// NewHelmOps constructs a new helmOps.
func NewHelmOps(ctx context.Context, kubeConfig *rest.Config, app *ApplicationConfig, token string, options Opt) (*HelmOps, error) {
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
	return ops, nil
}

// addApplicationLabelsToDeployment add application label to deployment or statefulset
func (h *HelmOps) addApplicationLabelsToDeployment() error {
	k8s, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}

	// add namespace to workspace
	patch := "{\"metadata\": {\"labels\":{\"kubesphere.io/workspace\":\"system-workspace\"}}}"
	_, err = k8s.CoreV1().Namespaces().Patch(h.ctx, h.app.Namespace,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return err
	}
	services := ToEntrancesLabel(h.app.Entrances)
	ports := ToAppTCPUDPPorts(h.app.Ports)

	acls := ToTailScaleACL(h.app.TailScaleACLs)

	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				constants.ApplicationNameLabel:      h.app.AppName,
				constants.ApplicationOwnerLabel:     h.app.OwnerName,
				constants.ApplicationTargetLabel:    h.app.Target,
				constants.ApplicationRunAsUserLabel: strconv.FormatBool(h.app.RunAsUser),
			},
			"annotations": map[string]string{
				constants.ApplicationIconLabel:       h.app.Icon,
				constants.ApplicationTitleLabel:      h.app.Title,
				constants.ApplicationVersionLabel:    h.app.Version,
				constants.ApplicationEntrancesKey:    services,
				constants.ApplicationPortsKey:        ports,
				constants.ApplicationSourceLabel:     h.options.Source,
				constants.ApplicationTailScaleACLKey: acls,
			},
		},
	}

	patchByte, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	patch = string(patchByte)

	// TODO: add ownerReferences of user
	deployment, err := k8s.AppsV1().Deployments(h.app.Namespace).Get(h.ctx, h.app.AppName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return h.tryToAddApplicationLabelsToStatefulSet(k8s, patch)
		}
		return err
	}

	_, err = k8s.AppsV1().Deployments(h.app.Namespace).Patch(h.ctx,
		deployment.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{})

	return err
}

func (h *HelmOps) tryToAddApplicationLabelsToStatefulSet(k8s *kubernetes.Clientset, patch string) error {
	statefulSet, err := k8s.AppsV1().StatefulSets(h.app.Namespace).Get(h.ctx, h.app.AppName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	_, err = k8s.AppsV1().StatefulSets(h.app.Namespace).Patch(h.ctx,
		statefulSet.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{})

	return err
}

func (h *HelmOps) status() (*helmrelease.Release, error) {
	statusClient := action.NewStatus(h.actionConfig)
	status, err := statusClient.Run(h.app.AppName)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (h *HelmOps) addLabelToNamespaceForDependClusterApp() error {
	k8s, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}

	labels := map[string]string{
		constants.ApplicationClusterDep: h.app.AppName,
	}
	patchData := map[string]interface{}{"metadata": map[string]map[string]string{"labels": labels}}
	patchBytes, _ := json.Marshal(patchData)
	_, err = k8s.CoreV1().Namespaces().Patch(h.ctx, h.app.Namespace,
		types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (h *HelmOps) setValues() (values map[string]interface{}, err error) {
	values = make(map[string]interface{})
	values["bfl"] = map[string]interface{}{
		"username": h.app.OwnerName,
	}
	zone, err := h.userZone()
	if err != nil {
		klog.Errorf("Failed to find user zone on crd err=%v", err)
	} else if zone != "" {
		values["user"] = map[string]interface{}{
			"zone": zone,
		}
	}

	entries := make(map[string]interface{})
	for i, entrance := range h.app.Entrances {
		var url string
		if len(h.app.Entrances) == 1 {
			url = fmt.Sprintf("%s.%s", h.app.AppID, zone)
		} else {
			url = fmt.Sprintf("%s%d.%s", h.app.AppID, i, zone)
		}
		entries[entrance.Name] = url
	}

	values["domain"] = entries
	userspace := make(map[string]interface{})
	h.app.Permission = parseAppPermission(h.app.Permission)
	for _, p := range h.app.Permission {
		switch perm := p.(type) {
		case AppDataPermission, AppCachePermission, UserDataPermission:

			// app requests app data permission
			// set .Values.schedule.nodeName and .Values.userspace.appCache to app
			// since app data on the bfl's local hostpath, app will schedule to the same node of bfl
			node, appCachePath, userspacePath, err := h.selectNode()
			if err != nil {
				klog.Errorf("Failed select node err=%v", err)
				return values, err
			}
			values["schedule"] = map[string]interface{}{
				"nodeName": node,
			}

			// appData = userspacePath + /Data
			// userData = userspacePath + /Home

			if perm == AppCacheRW {
				userspace["appCache"] = appCachePath
				if h.options.Source == "devbox" {
					userspace["appCache"] = filepath.Join(appCachePath, "devbox")
				}
			}
			if perm == UserDataRW {
				userspace["userData"] = fmt.Sprintf("%s/Home", userspacePath)
			}
			if perm == AppDataRW {
				appData := fmt.Sprintf("%s/Data", userspacePath)
				userspace["appData"] = appData
				if h.options.Source == "devbox" {
					userspace["appData"] = filepath.Join(appData, "devbox")
				}
			}

		case []SysDataPermission:
			appReg, err := h.registerAppPerm(perm)
			if err != nil {
				klog.Errorf("Failed to register err=%v", err)
				return values, err
			}

			values["os"] = map[string]interface{}{
				"appKey":    appReg.Data.AppKey,
				"appSecret": appReg.Data.AppSecret,
			}
		}
	}
	values["userspace"] = userspace

	// set service entrance for app that depend on cluster-scoped app
	type Service struct {
		EntranceName string
		Host         string
		Port         int
	}
	var services []Service
	appClient := versioned.NewForConfigOrDie(h.kubeConfig)
	apps, err := appClient.AppV1alpha1().Applications().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return values, err
	}

	clusterScopedAppNamespaces := sets.String{}

	for _, dep := range h.app.Dependencies {
		// if app is cluster-scoped get its host and port
		for _, app := range apps.Items {
			if dep.Type == constants.DependencyTypeApp && app.Spec.Name == dep.Name && app.Spec.Settings["clusterScoped"] == "true" {
				clusterScopedAppNamespaces.Insert(app.Spec.Namespace)
				for _, e := range app.Spec.Entrances {
					services = append(services, Service{
						Host:         e.Host + "." + app.Spec.Namespace,
						Port:         int(e.Port),
						EntranceName: e.Name,
					})
				}
			}
		}
	}
	// set cluster-scoped app's host and port to helm Values
	dep := make(map[string]interface{})
	for _, svc := range services {
		dep[fmt.Sprintf("%s_host", svc.EntranceName)] = svc.Host
		dep[fmt.Sprintf("%s_port", svc.EntranceName)] = svc.Port
	}
	values["dep"] = dep

	kClient, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return values, err
	}
	svcs := make(map[string]interface{})
	for ns := range clusterScopedAppNamespaces {
		servicesList, _ := kClient.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		for _, svc := range servicesList.Items {
			ports := make([]int32, 0)
			for _, p := range svc.Spec.Ports {
				ports = append(ports, p.Port)
			}
			svcs[fmt.Sprintf("%s_host", svc.Name)] = fmt.Sprintf("%s.%s", svc.Name, svc.Namespace)
			svcs[fmt.Sprintf("%s_ports", svc.Name)] = ports
		}
	}
	values["svcs"] = svcs
	klog.Info("svcs: ", svcs)

	var arch string
	nodes, err := kClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return values, err
	}
	for _, node := range nodes.Items {
		arch = node.Labels["kubernetes.io/arch"]
		break
	}
	values["cluster"] = map[string]interface{}{
		"arch": arch,
	}
	gpuType, err := utils.FindGpuTypeFromNodes(h.ctx, kClient)
	if err != nil {
		klog.Errorf("Failed to get gpuType err=%v", err)
		return values, err
	}
	values["GPU"] = map[string]interface{}{
		"Type": gpuType,
		"Cuda": os.Getenv("CUDA_VERSION"),
	}

	values["gpu"] = gpuType

	if h.app.OIDC.Enabled {
		err = h.createOIDCClient(values, zone, h.app.Namespace)
	}

	sharedLibPath := os.Getenv("SHARED_LIB_PATH")
	values["sharedlib"] = sharedLibPath

	admin, err := kubesphere.GetAdminUsername(context.TODO(), h.kubeConfig)
	if err != nil {
		return values, err
	}
	values["admin"] = admin

	rootPath := userspacev1.DefaultRootPath
	if os.Getenv(userspacev1.OlaresRootPath) != "" {
		rootPath = os.Getenv(userspacev1.OlaresRootPath)
	}
	values["rootPath"] = rootPath

	values["downloadCdnURL"] = os.Getenv("DOWNLOAD_CDN_URL")

	return values, err
}

func (h *HelmOps) userZone() (string, error) {
	return kubesphere.GetUserZone(h.ctx, h.kubeConfig, h.app.OwnerName)
}

func (h *HelmOps) registerAppPerm(perm []SysDataPermission) (*RegisterResp, error) {
	register := PermissionRegister{
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

	klog.Info("Sending app register request with body=%s url=%s", utils.PrettyJSON(string(body)), url)

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

	var regResp RegisterResp
	err = json.Unmarshal(resp.Body(), &regResp)
	if err != nil {
		klog.Error("Failed to unmarshal response body=%s err=%v", string(resp.Body()), err)
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
	register := PermissionRegister{
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
	if !utils.IsProtectedNamespace(h.app.Namespace) {
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

	_, appCacheHostDirs, err := utils.TryToGetAppdataDirFromDeployment(context.TODO(), h.app.Namespace, h.app.AppName, h.app.OwnerName)
	if err != nil {
		klog.Warning("get app cache error, ", err, ", ", h.app.AppName)
	}

	err = helm.UninstallCharts(h.actionConfig, h.app.AppName)
	if err != nil {
		return err
	}
	err = h.unregisterAppPerm()
	if err != nil {
		klog.Errorf("Failed to unregister app err=%v", err)
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

	if len(appCacheHostDirs) > 0 {
		klog.Info("clear app cache dirs, ", appCacheHostDirs)
		// FIXME: multi node version
		// terminusNonce, e := utils.GenTerminusNonce()
		// if e != nil {
		// 	klog.Errorf("Failed to generate terminus nonce err=%v", e)
		// } else {
		// 	c := resty.New().SetTimeout(2*time.Second).
		// 		SetHeader(constants.AuthorizationTokenKey, h.token).
		// 		SetHeader("Terminus-Nonce", terminusNonce)
		// 	nodes, e := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		// 	if e == nil {
		// 		for _, dir := range appCacheDirs {
		// 			for _, n := range nodes.Items {
		// 				URL := fmt.Sprintf(constants.AppDataDirURL, h.app.OwnerName, dir)
		// 				c.SetHeader("X-Terminus-Node", n.Name)
		// 				c.SetHeader("x-bfl-user", h.app.OwnerName)
		// 				res, e := c.R().Delete(URL)
		// 				if e != nil {
		// 					klog.Errorf("Failed to delete dir err=%v", e)
		// 				}
		// 				if res.StatusCode() != http.StatusOK {
		// 					klog.Infof("delete app cache failed with: %v", res.String())
		// 				}
		// 			}
		// 		}
		// 	} else {
		// 		klog.Error("Failed to get nodes err=%v", e)
		// 	}
		// }

		// mount the /olares/userdata/Cache/ to /Cache of app-service container
		baseDir := "/olares/userdata"
		for _, dir := range appCacheHostDirs {
			deleteDir := strings.TrimPrefix(dir, baseDir)
			if strings.HasPrefix(deleteDir, "/Cache") {
				klog.Info("remove dir in container, ", deleteDir)
				err := os.RemoveAll(deleteDir)
				if err != nil {
					klog.Warning("delete app cache error, ", err, ", ", dir)
				}
			} else {
				klog.Warning("invalidate path, ", dir)
			}
		}

	}

	if !utils.IsProtectedNamespace(h.app.Namespace) {
		return client.CoreV1().Namespaces().Delete(context.TODO(), h.app.Namespace, metav1.DeleteOptions{})
	}
	return nil
}

// Upgrade do a upgrade operation for release.
func (h *HelmOps) Upgrade() error {
	status, err := h.status()
	if err != nil {
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
		h.rollBack()
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
			h.rollBack()
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
		if appCfg, err := GetAppInstallationConfig(h.app.AppName, h.app.OwnerName); err != nil {
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
			Policies []Policy `json:"policies"`
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
		var appPolicies []AppPolicy
		for _, p := range policy.Policies {
			d, _ := time.ParseDuration(p.Duration)
			appPolicies = append(appPolicies, AppPolicy{
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
	name, _ := utils.FmtAppMgrName(h.app.AppName, h.app.OwnerName, h.app.Namespace)
	_, err = appClient.AppV1alpha1().Applications().Patch(h.ctx, name, types.MergePatchType, patchByte, metav1.PatchOptions{})
	if err != nil {
		return err
	}

	ok, err := h.waitForLaunch()
	if !ok {
		// canceled
		h.rollBack()
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
	ok := h.waitForStartUp()
	if !ok {
		return false, api.ErrStartUpFailed
	}

	req := reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: h.app.OwnerName,
	}}
	task.WQueue.(*task.Type).SetCompleted(req)

	klog.Infof("dequeue username:%s,appname:%s", h.app.OwnerName, h.app.AppName)

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
				if utils.TryConnect(host, strconv.Itoa(int(e.Port))) {
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
func (h *HelmOps) waitForStartUp() bool {
	timer := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-timer.C:
			startedUp := h.isStartUp()
			if startedUp {
				name, _ := utils.FmtAppMgrName(h.app.AppName, h.app.OwnerName, h.app.Namespace)
				appMgr := &appv1alpha1.ApplicationManager{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: appv1alpha1.ApplicationManagerSpec{
						AppName:  h.app.AppName,
						AppOwner: h.app.OwnerName,
					},
				}
				err := utils.UpdateAppState(h.ctx, appMgr, appv1alpha1.AppInitializing)
				if err != nil {
					klog.Errorf("update app state err=%v", err)
				}

				klog.Infof("time: %v, appState: %v", time.Now(), appv1alpha1.AppInitializing)
				return true
			}

		case <-h.ctx.Done():
			klog.Infof("Waiting for app startup canceled appName=%s", h.app.AppName)
			return false
		}
	}
}

func (h *HelmOps) isStartUp() bool {
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
			return false
		}
		labelSelector = metav1.FormatLabelSelector(sts.Spec.Selector)
	}
	pods, err := h.client.KubeClient.Kubernetes().CoreV1().Pods(h.app.Namespace).
		List(h.ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if len(pods.Items) == 0 {
		return false
	}
	for _, pod := range pods.Items {
		totalContainers := len(pod.Spec.Containers)
		startedContainers := 0
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]
			if *container.Started == true {
				startedContainers++
			}
		}
		if startedContainers == totalContainers {
			return true
		}
	}
	return false
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

func getApplicationPolicy(policies []AppPolicy, entrances []appv1alpha1.Entrance) (string, error) {
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

func parseAppPermission(data []AppPermission) []AppPermission {
	permissions := make([]AppPermission, 0)
	for _, p := range data {
		switch perm := p.(type) {
		case string:
			if perm == "appdata-perm" {
				permissions = append(permissions, AppDataRW)
			}
			if perm == "appcache-perm" {
				permissions = append(permissions, AppCacheRW)
			}
			if perm == "userdata-perm" {
				permissions = append(permissions, UserDataRW)
			}
		case AppDataPermission:
			permissions = append(permissions, AppDataRW)
		case AppCachePermission:
			permissions = append(permissions, AppCacheRW)
		case UserDataPermission:
			permissions = append(permissions, UserDataRW)
		case []SysDataPermission:
			permissions = append(permissions, p)
		case []interface{}:
			var sps []SysDataPermission
			for _, item := range perm {
				if m, ok := item.(map[string]interface{}); ok {
					var sp SysDataPermission
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
