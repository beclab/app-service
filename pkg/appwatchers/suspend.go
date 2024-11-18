package appwatchers

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/prometheus"

	"golang.org/x/exp/maps"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const suspendAnnotation = "bytetrade.io/suspend-by"
const suspendCauseAnnotation = "bytetrade.io/suspend-cause"

// SuspendTopApp suspends the top application in cluster level that with high load.
func SuspendTopApp(ctx context.Context, client client.Client) error {
	clusterCpuThreshold := getThreshold("CLUSTER_CPU_THRESHOLD")
	if clusterCpuThreshold == 0 {
		return nil
	}

	applications, err := getAllApplications(ctx, client, "")
	if err != nil {
		return err
	}

	toploadAppNamspaces, err := getTopLoadApps(ctx, applications, clusterCpuThreshold)
	if err != nil {
		return err
	}

	for _, ns := range toploadAppNamspaces {
		err = suspendApp(ctx, client, ns)
		if err != nil {
			return err
		}
	}

	return nil
}

// SuspendUserTopApp suspends the top application int user level that with high load.
func SuspendUserTopApp(ctx context.Context, client client.Client, user string) error {
	userCpuThreshold := getThreshold("USER_CPU_THRESHOLD")
	if userCpuThreshold == 0 {
		return nil
	}

	applications, err := getAllApplications(ctx, client, user)
	if err != nil {
		return err
	}
	topLoadUserAppNs, err := getUserTopLoadApps(ctx, applications, user, userCpuThreshold)
	if err != nil {
		return err
	}
	for _, ns := range topLoadUserAppNs {
		err = suspendApp(ctx, client, ns)
		if err != nil {
			return err
		}
	}
	return nil
}

func getAllApplications(ctx context.Context, client client.Client, user string) (map[string]*v1alpha1.Application, error) {
	var appList v1alpha1.ApplicationList
	err := client.List(ctx, &appList)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to list application err=%v", err)
		return nil, err
	}

	res := make(map[string]*v1alpha1.Application)
	for _, app := range appList.Items {
		if user != "" && app.Spec.Owner != user {
			continue
		}
		res[app.Spec.Namespace] = &app
	}

	return res, nil
}

func getTopLoadApps(ctx context.Context, applications map[string]*v1alpha1.Application, clusterCpuThreshold int64) ([]string, error) {
	prom, err := prometheus.New(prometheus.Endpoint)
	if err != nil {
		klog.Errorf("Failed to connect to prometheus service err=%v", err)
		return nil, err
	}
	metrics := prom.GetNamedMetrics(ctx, []string{"cluster_cpu_utilisation"}, time.Now(), prometheus.QueryOptions{Level: prometheus.LevelCluster})
	disableCpuMonitor := false
	for _, m := range metrics {
		if m.MetricName == "cluster_cpu_utilisation" && prometheus.GetValue(&m) < float64(clusterCpuThreshold)*0.01 {
			disableCpuMonitor = true
		}
	}

	metrics = prom.GetNamedMetrics(ctx, []string{"namespaces_memory_usage", "namespaces_cpu_usage"}, time.Now(), prometheus.QueryOptions{Level: prometheus.LevelCluster})
	namespaces := make(map[string]bool)
	for _, m := range metrics {
		if m.MetricName == "namespaces_cpu_usage" && disableCpuMonitor {
			continue
		}
		sorted := prometheus.GetSortedNamespaceMetrics(&m)
		// find top 2 non-builtin apps
		top := 0
		for _, s := range sorted {
			if _, ok := applications[s.Namespace]; ok && !strings.HasPrefix(s.Namespace, "user-space-") {
				namespaces[s.Namespace] = true
				if len(namespaces) == 2 {
					return maps.Keys(namespaces), nil
				}
				top++
			}

			if top == 2 {
				break
			}
		}
	} // end of metrics loop

	return maps.Keys(namespaces), nil
}

func suspendApp(ctx context.Context, cli client.Client, namespace string) error {
	suspend := func(list client.ObjectList) error {
		err := cli.List(ctx, list, client.InNamespace(namespace))
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get workload namespace=%s err=%v", namespace, err)
			return err
		}

		listObjects, err := apimeta.ExtractList(list)
		if err != nil {
			klog.Errorf("Failed to extract list namespace=%s err=%v", namespace, err)
			return err
		}

		var zeroReplica int32 = 0
		for _, w := range listObjects {
			workloadName := ""
			switch workload := w.(type) {
			case *appsv1.Deployment:
				workload.Annotations[suspendAnnotation] = "app-service"
				workload.Annotations[suspendCauseAnnotation] = "system high load"
				workload.Spec.Replicas = &zeroReplica
				workloadName = workload.Namespace + "/" + workload.Name
			case *appsv1.StatefulSet:
				workload.Annotations[suspendAnnotation] = "app-service"
				workload.Annotations[suspendCauseAnnotation] = "system high load"
				workload.Spec.Replicas = &zeroReplica
				workloadName = workload.Namespace + "/" + workload.Name
			}

			klog.Infof("Try to suspend workload=%s", workloadName)
			err := cli.Update(ctx, w.(client.Object))
			if err != nil {
				klog.Errorf("Failed to scale workload=%s err=%v", workloadName, err)
				return err
			}

			klog.Infof("Success to suspend workload=%s", workloadName)
		} // end list object loop

		return nil
	} // end of suspend func

	var deploymentList appsv1.DeploymentList
	err := suspend(&deploymentList)
	if err != nil {
		return err
	}

	var statefulsetList appsv1.StatefulSetList
	err = suspend(&statefulsetList)

	return err
}

func getUserTopLoadApps(ctx context.Context, applications map[string]*v1alpha1.Application, user string, userCpuThreshold int64) ([]string, error) {
	prom, err := prometheus.New(prometheus.Endpoint)
	if err != nil {
		klog.Errorf("Failed to connect to prometheus service err=%v", err)
		return nil, err
	}
	opts := prometheus.QueryOptions{
		Level:    prometheus.LevelUser,
		UserName: user,
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	userCpuLimitStr, err := kubesphere.GetUserCPULimit(ctx, kubeConfig, user)
	if err != nil {
		return nil, err
	}
	c, _ := resource.ParseQuantity(userCpuLimitStr)

	metrics := prom.GetNamedMetrics(ctx, []string{"user_cpu_usage", "user_memory_usage"}, time.Now(), opts)
	namespaces := make(map[string]bool)
	for _, m := range metrics {
		if m.MetricName == "user_cpu_usage" && prometheus.GetValue(&m) < c.AsApproximateFloat64()*float64(userCpuThreshold)*0.01 {
			continue
		}
		sorted := prometheus.GetSortedUserMetrics(&m)
		// find top 2 non-builtin apps
		top := 0
		for _, s := range sorted {
			if app, ok := applications[s.User]; ok && !strings.HasPrefix(app.Namespace, "user-space-") {
				namespaces[app.Namespace] = true
				if len(namespaces) == 2 {
					return maps.Keys(namespaces), nil
				}
				top++
			}

			if top == 2 {
				break
			}
		}
	} // end of metrics loop

	return maps.Keys(namespaces), nil
}

func getThreshold(name string) int64 {
	clusterCpuThresholdStr := os.Getenv(name)
	if clusterCpuThresholdStr == "0" {
		return 0
	}
	clusterCpuThreshold, err := strconv.ParseInt(clusterCpuThresholdStr, 10, 64)
	if err != nil || clusterCpuThreshold < 90 {
		clusterCpuThreshold = 90
	}
	return clusterCpuThreshold
}
