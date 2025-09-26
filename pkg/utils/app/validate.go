package app

import (
	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	v1alpha1client "bytetrade.io/web3os/app-service/pkg/client/clientset/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned/scheme"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/prometheus"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/upgrade"
	"bytetrade.io/web3os/app-service/pkg/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CheckChartSource(source api.AppSource) error {
	if source != api.Market && source != api.Custom && source != api.DevBox && source != api.System {
		return fmt.Errorf("unsupported chart source: %s", source)
	}
	return nil
}

// CheckDependencies check application dependencies, returns unsatisfied dependency.
func CheckDependencies(ctx context.Context, ctrlClient client.Client, deps []appcfg.Dependency, owner string, checkAll bool) ([]appcfg.Dependency, error) {
	unSatisfiedDeps := make([]appcfg.Dependency, 0)
	var appList v1alpha1.ApplicationList
	err := ctrlClient.List(ctx, &appList)
	if err != nil {
		return unSatisfiedDeps, err
	}

	appToVersion := make(map[string]string)
	appNames := make([]string, 0, len(appList.Items))
	for _, app := range appList.Items {
		clusterScoped, _ := strconv.ParseBool(app.Spec.Settings["clusterScoped"])
		// add app name to list if app is cluster scoped or owner equal app.Spec.Name
		if clusterScoped || owner == app.Spec.Owner {
			appNames = append(appNames, app.Spec.Name)
			appToVersion[app.Spec.Name] = app.Spec.Settings["version"]
		}
	}
	set := sets.NewString(appNames...)

	for _, dep := range deps {
		if dep.Type == constants.DependencyTypeSystem {
			terminus, err := upgrade.GetTerminusVersion(ctx, ctrlClient)
			if err != nil {
				return unSatisfiedDeps, err
			}

			if !utils.MatchVersion(terminus.Spec.Version, dep.Version) {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("terminus version %s not match dependency %s", terminus.Spec.Version, dep.Version)
				}
			}
		}
		if dep.Type == constants.DependencyTypeApp {
			if dep.SelfRely == true {
				continue
			}
			if !set.Has(dep.Name) && dep.Mandatory {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("dependency application %s not existed", dep.Name)
				}
			}
			if !utils.MatchVersion(appToVersion[dep.Name], dep.Version) && dep.Mandatory {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("%s version: %s not match dependency %s", dep.Name, appToVersion[dep.Name], dep.Version)
				}
			}
		}
	}
	if len(unSatisfiedDeps) > 0 {
		return unSatisfiedDeps, fmt.Errorf("some dependency not satisfied")
	}
	return unSatisfiedDeps, nil
}

func CheckDependencies2(ctx context.Context, ctrlClient client.Client, deps []appcfg.Dependency, owner string, checkAll bool) error {
	unSatisfiedDeps, err := CheckDependencies(ctx, ctrlClient, deps, owner, checkAll)
	if err != nil {
		return err
	}
	if len(unSatisfiedDeps) > 0 {
		return FormatDependencyError(unSatisfiedDeps)
	}
	return nil
}

func FormatDependencyError(deps []appcfg.Dependency) error {
	var systemDeps, appDeps []string

	for _, dep := range deps {
		depInfo := fmt.Sprintf("%s version=%s",
			dep.Name, dep.Version)

		if dep.Type == "system" {
			systemDeps = append(systemDeps, depInfo)
		} else if dep.Type == "application" {
			appDeps = append(appDeps, depInfo)
		}
	}

	var errMsg strings.Builder
	errMsg.WriteString("Missing dependencies:\n")

	if len(systemDeps) > 0 {
		errMsg.WriteString("\nSystem Dependencies:\n")
		for _, dep := range systemDeps {
			errMsg.WriteString(fmt.Sprintf("- %s\n", dep))
		}
	}

	if len(appDeps) > 0 {
		errMsg.WriteString("\nApplication Dependencies:\n")
		for _, dep := range appDeps {
			errMsg.WriteString(fmt.Sprintf("- %s\n", dep))
		}
	}

	return errors.New(errMsg.String())
}

func CheckConflicts(ctx context.Context, conflicts []appcfg.Conflict, owner string) error {
	installedConflictApp := make([]string, 0)
	client, err := utils.GetClient()
	if err != nil {
		return err
	}
	appSet := sets.NewString()
	applist, err := client.AppV1alpha1().Applications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, app := range applist.Items {
		if app.Spec.Owner != owner {
			continue
		}
		appSet.Insert(app.Spec.Name)
	}
	for _, cf := range conflicts {
		if cf.Type != "application" {
			continue
		}
		if appSet.Has(cf.Name) {
			installedConflictApp = append(installedConflictApp, cf.Name)
		}
	}
	if len(installedConflictApp) > 0 {
		return fmt.Errorf("this app conflict with those installed app: %v", installedConflictApp)
	}
	return nil
}

func CheckCfgFileVersion(version, constraint string) error {
	if !utils.MatchVersion(version, constraint) {
		return fmt.Errorf("olaresManifest.version must >= %s", constraint)
	}
	return nil
}

func CheckNamespace(ns string) error {
	if IsForbidNamespace(ns) {
		return fmt.Errorf("unsupported namespace: %s", ns)
	}
	return nil
}

func CheckUserRole(appConfig *appcfg.ApplicationConfig, owner string) error {
	role, err := kubesphere.GetUserRole(context.TODO(), owner)
	if err != nil {
		return err
	}
	if (appConfig.OnlyAdmin || appConfig.AppScope.ClusterScoped) && role != "owner" && role != "admin" {
		return errors.New("only admin user can install this app")
	}
	return nil
}

// CheckAppRequirement check if the cluster has enough resources for application install/upgrade.
func CheckAppRequirement(token string, appConfig *appcfg.ApplicationConfig) (string, error) {
	metrics, _, err := GetClusterResource(token)
	if err != nil {
		return "", err
	}

	klog.Infof("Current resource=%s", utils.PrettyJSON(metrics))
	klog.Infof("App required resource=%s", utils.PrettyJSON(appConfig.Requirement))

	switch {
	case appConfig.Requirement.Disk != nil &&
		appConfig.Requirement.Disk.CmpInt64(int64(metrics.Disk.Total-metrics.Disk.Usage)) > 0:
		return "disk", errors.New("The app's DISK requirement cannot be satisfied")
	case appConfig.Requirement.Memory != nil &&
		appConfig.Requirement.Memory.CmpInt64(int64(metrics.Memory.Total*0.9-metrics.Memory.Usage)) > 0:
		return "memory", errors.New("The app's MEMORY requirement cannot be satisfied")
	case appConfig.Requirement.CPU != nil:
		availableCPU, _ := resource.ParseQuantity(strconv.FormatFloat(metrics.CPU.Total*0.9-metrics.CPU.Usage, 'f', -1, 64))
		if appConfig.Requirement.CPU.Cmp(availableCPU) > 0 {
			return "cpu", errors.New("The app's CPU requirement cannot be satisfied")
		}
	case appConfig.Requirement.GPU != nil && !appConfig.Requirement.GPU.IsZero() &&
		metrics.GPU.Total <= 0:
		return "gpu", errors.New("The app's GPU requirement cannot be satisfied")
	}

	allocatedResources, err := getRequestResources()
	if err != nil {
		return "", err
	}
	if len(allocatedResources) == 1 {
		sufficientCPU, sufficientMemory := false, false
		if appConfig.Requirement.CPU == nil {
			sufficientCPU = true
		}
		if appConfig.Requirement.Memory == nil {
			sufficientMemory = true
		}
		for _, v := range allocatedResources {
			if appConfig.Requirement.CPU != nil {
				if v.cpu.allocatable.Cmp(*appConfig.Requirement.CPU) > 0 {
					sufficientCPU = true
				}
			}
			if appConfig.Requirement.Memory != nil {
				if v.memory.allocatable.Cmp(*appConfig.Requirement.Memory) > 0 {
					sufficientMemory = true
				}
			}
		}
		if !sufficientCPU {
			return "cpu", errors.New("The app's CPU requirement specified in the kubernetes requests cannot be satisfied")
		}
		if !sufficientMemory {
			return "memory", errors.New("The app's MEMORY requirement specified in the kubernetes requests cannot be satisfied")
		}
	}

	return "", nil
}

func getRequestResources() (map[string]resources, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	allocatedResources := make(map[string]resources)
	for _, node := range nodes.Items {
		allocatedResources[node.Name] = resources{cpu: usage{allocatable: node.Status.Allocatable.Cpu()},
			memory: usage{allocatable: node.Status.Allocatable.Memory()}}
		fieldSelector := fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", node.Name)
		pods, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
			FieldSelector: fieldSelector,
		})
		if err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.Containers {
				allocatedResources[node.Name].cpu.allocatable.Sub(*container.Resources.Requests.Cpu())
				allocatedResources[node.Name].memory.allocatable.Sub(*container.Resources.Requests.Memory())
			}
		}
	}
	return allocatedResources, nil
}

type resources struct {
	cpu    usage
	memory usage
}

type usage struct {
	allocatable *resource.Quantity
}

// GetClusterResource returns cluster resource metrics and cluster arches.
func GetClusterResource(token string) (*prometheus.ClusterMetrics, []string, error) {
	supportArch := make([]string, 0)
	arches := sets.String{}

	config := rest.Config{
		Host:        constants.KubeSphereAPIHost,
		BearerToken: token,
		APIPath:     "/kapis",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &schema.GroupVersion{
				Group:   "monitoring.kubesphere.io",
				Version: "v1alpha3",
			},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, supportArch, err
	}

	metricParam := "cluster_cpu_usage|cluster_cpu_total|cluster_memory_usage_wo_cache|cluster_memory_total|cluster_disk_size_usage|cluster_disk_size_capacity|cluster_pod_running_count|cluster_pod_quota$"

	client.Client.Timeout = 2 * time.Second
	res := client.Get().Resource("cluster").
		Param("metrics_filter", metricParam).Do(context.TODO())

	if res.Error() != nil {
		return nil, supportArch, res.Error()
	}

	var metrics Metrics
	data, err := res.Raw()
	if err != nil {
		return nil, supportArch, err
	}

	err = json.Unmarshal(data, &metrics)
	if err != nil {
		return nil, supportArch, err
	}

	var clusterMetrics prometheus.ClusterMetrics
	for _, m := range metrics.Results {
		switch m.MetricName {
		case "cluster_cpu_usage":
			clusterMetrics.CPU.Usage = getValue(&m)
		case "cluster_cpu_total":
			clusterMetrics.CPU.Total = getValue(&m)

		case "cluster_disk_size_usage":
			clusterMetrics.Disk.Usage = getValue(&m)
		case "cluster_disk_size_capacity":
			clusterMetrics.Disk.Total = getValue(&m)

		case "cluster_memory_total":
			clusterMetrics.Memory.Total = getValue(&m)
		case "cluster_memory_usage_wo_cache":
			clusterMetrics.Memory.Usage = getValue(&m)
		}
	}

	// get k8s client with node list privileges
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, supportArch, err
	}

	k8sClient, err := v1alpha1client.NewKubeClient("", kubeConfig)
	if err != nil {
		klog.Errorf("Failed to create k8s client err=%v", err)
	} else {
		nodes, err := k8sClient.Kubernetes().CoreV1().Nodes().List(
			context.TODO(),
			metav1.ListOptions{},
		)

		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to list node err=%v", err)
		}

		if apierrors.IsNotFound(err) {
			clusterMetrics.GPU.Total = 0
		} else {
			var total float64 = 0
			for _, n := range nodes.Items {
				arches.Insert(n.Labels["kubernetes.io/arch"])
				if quantity, ok := n.Status.Capacity[constants.NvidiaGPU]; ok {
					total += quantity.AsApproximateFloat64()
				} else if quantity, ok = n.Status.Capacity[constants.VirtAiTechVGPU]; ok {
					total += quantity.AsApproximateFloat64()
				}
			}

			clusterMetrics.GPU.Total = total
		}

	}
	for arch := range arches {
		supportArch = append(supportArch, arch)
	}
	return &clusterMetrics, supportArch, nil
}

func getValue(m *kubesphere.Metric) float64 {
	if len(m.MetricData.MetricValues) == 0 {
		return 0.0
	}
	return m.MetricData.MetricValues[0].Sample[1]
}

// CheckUserResRequirement check if the user has enough resources for application install/upgrade.
func CheckUserResRequirement(ctx context.Context, appConfig *appcfg.ApplicationConfig, username string) (string, error) {
	metrics, err := prometheus.GetCurUserResource(ctx, username)
	if err != nil {
		return "", err
	}
	switch {
	case appConfig.Requirement.Memory != nil && metrics.Memory.Total != 0 &&
		appConfig.Requirement.Memory.CmpInt64(int64(metrics.Memory.Total*0.9-metrics.Memory.Usage)) > 0:
		return "memory", errors.New("The user's app MEMORY requirement cannot be satisfied")
	case appConfig.Requirement.CPU != nil && metrics.CPU.Total != 0:
		availableCPU, _ := resource.ParseQuantity(strconv.FormatFloat(metrics.CPU.Total*0.9-metrics.CPU.Usage, 'f', -1, 64))
		if appConfig.Requirement.CPU.Cmp(availableCPU) > 0 {
			return "cpu", errors.New("The user's app CPU requirement cannot be satisfied")
		}
	}
	return "", nil
}

func CheckMiddlewareRequirement(ctx context.Context, ctrlClient client.Client, middleware *tapr.Middleware) (bool, error) {
	if middleware != nil {
		if middleware.MongoDB != nil {
			var am v1alpha1.ApplicationManager
			err := ctrlClient.Get(ctx, types.NamespacedName{Name: "mongodb-middleware-mongodb"}, &am)
			if err != nil {
				return false, err
			}
			if am.Status.State == "running" {
				return true, nil
			}
			return false, nil
		}
		if middleware.Minio != nil {
			var am v1alpha1.ApplicationManager
			err := ctrlClient.Get(ctx, types.NamespacedName{Name: "minio-middleware-minio"}, &am)
			if err != nil {
				return false, err
			}
			if am.Status.State == "running" {
				return true, nil
			}
			return false, nil
		}
		return true, nil
	}
	return true, nil
}

func CheckAppEnvs(ctx context.Context, ctrlClient client.Client, envs []sysv1alpha1.AppEnvVar, owner string) (*api.AppEnvCheckResult, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	result := new(api.AppEnvCheckResult)
	referencedEnvs := make(map[string]string)
	var once sync.Once
	for _, env := range envs {
		if env.ValueFrom != nil && env.ValueFrom.EnvName != "" {
			var listErr error
			once.Do(func() {
				sysenvs := new(sysv1alpha1.SystemEnvList)
				listErr = ctrlClient.List(ctx, sysenvs)
				if listErr != nil {
					return
				}
				userenvs := new(sysv1alpha1.UserEnvList)
				listErr = ctrlClient.List(ctx, userenvs, client.InNamespace(utils.UserspaceName(owner)))
				for _, sysenv := range sysenvs.Items {
					referencedEnvs[sysenv.EnvName] = sysenv.GetEffectiveValue()
				}
				for _, userenv := range userenvs.Items {
					referencedEnvs[userenv.EnvName] = userenv.GetEffectiveValue()
				}
			})
			if listErr != nil {
				return nil, fmt.Errorf("failed to list referenced envs: %s", listErr)
			}
			if value, ok := referencedEnvs[env.ValueFrom.EnvName]; !ok || value == "" {
				result.MissingRefs = append(result.MissingRefs, env)
			}
			continue
		}
		effectiveValue := env.GetEffectiveValue()
		if env.Required && effectiveValue == "" {
			result.MissingValues = append(result.MissingValues, env)
			continue
		}
		if err := utils.CheckEnvValueByType(effectiveValue, env.Type); err != nil {
			result.InvalidValues = append(result.InvalidValues, env)
		}
	}
	if len(result.MissingValues) > 0 || len(result.InvalidValues) > 0 || len(result.MissingRefs) > 0 {
		return result, nil
	}
	return nil, nil

}
