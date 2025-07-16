package appinstaller

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/errcode"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	userspacev1 "bytetrade.io/web3os/app-service/pkg/users/userspace/v1"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"helm.sh/helm/v3/pkg/action"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"strconv"
)

type InstallStrategyV1 struct {
	helmOps *HelmOps
}

var _ InstallStrategy = &InstallStrategyV1{}

func NewInstallV1(helmOps *HelmOps) InstallStrategy {
	return &InstallStrategyV1{
		helmOps: helmOps,
	}
}

func (i *InstallStrategyV1) Install() error {
	var err error
	values, err := i.setValues()
	if err != nil {
		klog.Errorf("set values err %v", err)
		return err
	}
	namespace := fmt.Sprintf("%s-%s", "user-system", i.helmOps.app.OwnerName)
	if err := tapr.Apply(i.helmOps.app.Middleware, i.helmOps.kubeConfig, i.helmOps.app.AppName, i.helmOps.app.Namespace,
		namespace, i.helmOps.token, i.helmOps.app.ChartsName, i.helmOps.app.OwnerName, values); err != nil {
		klog.Errorf("Failed to apply middleware err=%v", err)
		return err
	}
	err = i.install(values)
	if err != nil && !errors.Is(err, driver.ErrReleaseExists) {
		klog.Errorf("Failed to install chart err=%v", err)
		i.helmOps.Uninstall()
		return err
	}
	err = i.addApplicationLabelsToDeployment()
	if err != nil {
		i.helmOps.Uninstall()
		return err
	}

	isDepClusterScopedApp := false
	client, err := versioned.NewForConfig(i.helmOps.kubeConfig)
	if err != nil {
		return err
	}
	apps, err := client.AppV1alpha1().Applications().List(i.helmOps.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, dep := range i.helmOps.app.Dependencies {
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
		err = i.addLabelToNamespaceForDependClusterApp()
		if err != nil {
			i.helmOps.Uninstall()
			return err
		}
	}

	ok, err := i.helmOps.waitForStartUp()
	if err != nil && errors.Is(err, errcode.ErrPodPending) {
		return err
	}
	if !ok {
		i.helmOps.Uninstall()
		return err
	}
	return nil
}

func (i *InstallStrategyV1) install(values map[string]interface{}) error {
	_, err := i.helmOps.status()
	if err == nil {
		return driver.ErrReleaseExists
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return helm.InstallCharts(i.helmOps.ctx, i.helmOps.actionConfig, i.helmOps.settings, i.helmOps.app.AppName, i.helmOps.app.ChartsName, i.helmOps.app.RepoURL, i.helmOps.app.Namespace, values)
	}
	return err
}

// addApplicationLabelsToDeployment add application label to deployment or statefulset
func (i *InstallStrategyV1) addApplicationLabelsToDeployment() error {
	k8s, err := kubernetes.NewForConfig(i.helmOps.kubeConfig)
	if err != nil {
		return err
	}

	// add namespace to workspace
	patch := "{\"metadata\": {\"labels\":{\"kubesphere.io/workspace\":\"system-workspace\"}}}"
	_, err = k8s.CoreV1().Namespaces().Patch(i.helmOps.ctx, i.helmOps.app.Namespace,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return err
	}
	services := ToEntrancesLabel(i.helmOps.app.Entrances)
	ports := ToAppTCPUDPPorts(i.helmOps.app.Ports)

	tailScale := ToTailScale(i.helmOps.app.TailScale)

	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				constants.ApplicationNameLabel:      i.helmOps.app.AppName,
				constants.ApplicationOwnerLabel:     i.helmOps.app.OwnerName,
				constants.ApplicationTargetLabel:    i.helmOps.app.Target,
				constants.ApplicationRunAsUserLabel: strconv.FormatBool(i.helmOps.app.RunAsUser),
			},
			"annotations": map[string]string{
				constants.ApplicationIconLabel:    i.helmOps.app.Icon,
				constants.ApplicationTitleLabel:   i.helmOps.app.Title,
				constants.ApplicationVersionLabel: i.helmOps.app.Version,
				constants.ApplicationEntrancesKey: services,
				constants.ApplicationPortsKey:     ports,
				constants.ApplicationSourceLabel:  i.helmOps.options.Source,
				constants.ApplicationTailScaleKey: tailScale,
				constants.ApplicationRequiredGPU:  i.helmOps.app.RequiredGPU,
			},
		},
	}

	patchByte, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	patch = string(patchByte)

	// TODO: add ownerReferences of user
	deployment, err := k8s.AppsV1().Deployments(i.helmOps.app.Namespace).Get(i.helmOps.ctx, i.helmOps.app.AppName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return i.tryToAddApplicationLabelsToStatefulSet(k8s, patch)
		}
		return err
	}

	_, err = k8s.AppsV1().Deployments(i.helmOps.app.Namespace).Patch(i.helmOps.ctx,
		deployment.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{})

	return err
}
func (i *InstallStrategyV1) tryToAddApplicationLabelsToStatefulSet(k8s *kubernetes.Clientset, patch string) error {
	statefulSet, err := k8s.AppsV1().StatefulSets(i.helmOps.app.Namespace).Get(i.helmOps.ctx, i.helmOps.app.AppName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	_, err = k8s.AppsV1().StatefulSets(i.helmOps.app.Namespace).Patch(i.helmOps.ctx,
		statefulSet.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{})

	return err
}

func (i *InstallStrategyV1) status() (*helmrelease.Release, error) {
	statusClient := action.NewStatus(i.helmOps.actionConfig)
	status, err := statusClient.Run(i.helmOps.app.AppName)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (i *InstallStrategyV1) addLabelToNamespaceForDependClusterApp() error {
	k8s, err := kubernetes.NewForConfig(i.helmOps.kubeConfig)
	if err != nil {
		return err
	}

	labels := map[string]string{
		constants.ApplicationClusterDep: i.helmOps.app.AppName,
	}
	patchData := map[string]interface{}{"metadata": map[string]map[string]string{"labels": labels}}
	patchBytes, _ := json.Marshal(patchData)
	_, err = k8s.CoreV1().Namespaces().Patch(i.helmOps.ctx, i.helmOps.app.Namespace,
		types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (i *InstallStrategyV1) setValues() (values map[string]interface{}, err error) {
	values = make(map[string]interface{})
	values["bfl"] = map[string]interface{}{
		"username": i.helmOps.app.OwnerName,
	}
	zone, err := i.helmOps.userZone()
	if err != nil {
		klog.Errorf("Failed to find user zone on crd err=%v", err)
	} else if zone != "" {
		values["user"] = map[string]interface{}{
			"zone": zone,
		}
	}

	entries := make(map[string]interface{})
	for index, entrance := range i.helmOps.app.Entrances {
		var url string
		if len(i.helmOps.app.Entrances) == 1 {
			url = fmt.Sprintf("%s.%s", i.helmOps.app.AppID, zone)
		} else {
			url = fmt.Sprintf("%s%d.%s", i.helmOps.app.AppID, index, zone)
		}
		entries[entrance.Name] = url
	}

	values["domain"] = entries
	userspace := make(map[string]interface{})
	i.helmOps.app.Permission = parseAppPermission(i.helmOps.app.Permission)
	for _, p := range i.helmOps.app.Permission {
		switch perm := p.(type) {
		case appcfg.AppDataPermission, appcfg.AppCachePermission, appcfg.UserDataPermission:

			// app requests app data permission
			// set .Values.schedule.nodeName and .Values.userspace.appCache to app
			// since app data on the bfl's local hostpath, app will schedule to the same node of bfl
			node, appCachePath, userspacePath, err := i.helmOps.selectNode()
			if err != nil {
				klog.Errorf("Failed select node err=%v", err)
				return values, err
			}
			values["schedule"] = map[string]interface{}{
				"nodeName": node,
			}

			// appData = userspacePath + /Data
			// userData = userspacePath + /Home

			if perm == appcfg.AppCacheRW {
				userspace["appCache"] = appCachePath
				if i.helmOps.options.Source == "devbox" {
					userspace["appCache"] = filepath.Join(appCachePath, "studio")
				}
			}
			if perm == appcfg.UserDataRW {
				userspace["userData"] = fmt.Sprintf("%s/Home", userspacePath)
			}
			if perm == appcfg.AppDataRW {
				appData := fmt.Sprintf("%s/Data", userspacePath)
				userspace["appData"] = appData
				if i.helmOps.options.Source == "devbox" {
					userspace["appData"] = filepath.Join(appData, "studio")
				}
			}

		case []appcfg.SysDataPermission:
			appReg, err := i.helmOps.registerAppPerm(perm)
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
	appClient := versioned.NewForConfigOrDie(i.helmOps.kubeConfig)
	apps, err := appClient.AppV1alpha1().Applications().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return values, err
	}

	clusterScopedAppNamespaces := sets.String{}

	for _, dep := range i.helmOps.app.Dependencies {
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

	kClient, err := kubernetes.NewForConfig(i.helmOps.kubeConfig)
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
	gpuType, err := utils.FindGpuTypeFromNodes(nodes)
	if err != nil {
		klog.Errorf("Failed to get gpuType err=%v", err)
		return values, err
	}
	values["GPU"] = map[string]interface{}{
		"Type": gpuType,
		"Cuda": os.Getenv("CUDA_VERSION"),
	}

	values["gpu"] = gpuType

	if i.helmOps.app.OIDC.Enabled {
		err = i.helmOps.createOIDCClient(values, zone, i.helmOps.app.Namespace)
		if err != nil {
			klog.Errorf("Failed to create OIDCClient err=%v", err)
			return values, err
		}
	}

	sharedLibPath := os.Getenv("SHARED_LIB_PATH")
	values["sharedlib"] = sharedLibPath

	admin, err := kubesphere.GetAdminUsername(context.TODO(), i.helmOps.kubeConfig)
	if err != nil {
		return values, err
	}
	values["admin"] = admin

	isAdmin, err := kubesphere.IsAdmin(context.TODO(), i.helmOps.kubeConfig, i.helmOps.app.OwnerName)
	if err != nil {
		return values, err
	}
	values["isAdmin"] = isAdmin

	rootPath := userspacev1.DefaultRootPath
	if os.Getenv(userspacev1.OlaresRootPath) != "" {
		rootPath = os.Getenv(userspacev1.OlaresRootPath)
	}
	values["rootPath"] = rootPath

	values["downloadCdnURL"] = os.Getenv("DOWNLOAD_CDN_URL")

	return values, err
}
