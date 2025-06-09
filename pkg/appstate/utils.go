package appstate

import (
	"context"
	"fmt"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const suspendAnnotation = "bytetrade.io/suspend-by"
const suspendCauseAnnotation = "bytetrade.io/suspend-cause"

type portKey struct {
	port     int32
	protocol string
}

func setExposePorts(ctx context.Context, appConfig *appcfg.ApplicationConfig) error {
	existPorts := make(map[portKey]struct{})
	client, err := utils.GetClient()
	if err != nil {
		return err
	}
	apps, err := client.AppV1alpha1().Applications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, app := range apps.Items {
		for _, p := range app.Spec.Ports {
			protos := []string{p.Protocol}
			if p.Protocol == "" {
				protos = []string{"tcp", "udp"}
			}
			for _, proto := range protos {
				key := portKey{
					port:     p.ExposePort,
					protocol: proto,
				}
				existPorts[key] = struct{}{}
			}
		}
	}
	klog.Infof("existPorts: %v", existPorts)

	for i := range appConfig.Ports {
		port := &appConfig.Ports[i]
		if port.ExposePort == 0 {
			var exposePort int32
			protos := []string{port.Protocol}
			if port.Protocol == "" {
				protos = []string{"tcp", "udp"}
			}

			for i := 0; i < 5; i++ {
				exposePort, err = genPort(protos)
				if err != nil {
					continue
				}
				for _, proto := range protos {
					key := portKey{port: exposePort, protocol: proto}
					if _, ok := existPorts[key]; !ok && err == nil {
						break
					}
				}
			}
			for _, proto := range protos {
				key := portKey{port: exposePort, protocol: proto}
				if _, ok := existPorts[key]; ok || err != nil {
					return fmt.Errorf("%d port is not available", key.port)
				}
				existPorts[key] = struct{}{}
				port.ExposePort = exposePort
			}
		}
	}

	// add exposePort to tailscale acls
	for i := range appConfig.Ports {
		if appConfig.Ports[i].AddToTailscaleAcl {
			appConfig.TailScale.ACLs = append(appConfig.TailScale.ACLs, appv1alpha1.ACL{
				Proto: appConfig.Ports[i].Protocol,
				Dst:   []string{fmt.Sprintf("*:%d", appConfig.Ports[i].ExposePort)},
			})
		}
	}
	return nil
}

func genPort(protos []string) (int32, error) {
	exposePort := int32(rand.IntnRange(33333, 36789))
	for _, proto := range protos {
		if !utils.IsPortAvailable(proto, int(exposePort)) {
			return 0, fmt.Errorf("failed to allocate an available port after 5 attempts")
		}
	}
	return exposePort, nil
}

func suspendOrResumeApp(ctx context.Context, cli client.Client, am *appv1alpha1.ApplicationManager, replicas int32) error {
	suspend := func(list client.ObjectList) error {
		namespace := am.Spec.AppNamespace
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
		check := func(appName, deployName string) bool {
			if namespace == fmt.Sprintf("user-space-%s", am.Spec.AppOwner) ||
				namespace == fmt.Sprintf("user-system-%s", am.Spec.AppOwner) ||
				namespace == "os-system" {
				if appName == deployName {
					return true
				}
			} else {
				return true
			}
			return false
		}

		//var zeroReplica int32 = 0
		for _, w := range listObjects {
			workloadName := ""
			switch workload := w.(type) {
			case *appsv1.Deployment:
				if check(am.Spec.AppName, workload.Name) {
					workload.Annotations[suspendAnnotation] = "app-service"
					workload.Annotations[suspendCauseAnnotation] = "user operate"
					workload.Spec.Replicas = &replicas
					workloadName = workload.Namespace + "/" + workload.Name
				}
			case *appsv1.StatefulSet:
				if check(am.Spec.AppName, workload.Name) {
					workload.Annotations[suspendAnnotation] = "app-service"
					workload.Annotations[suspendCauseAnnotation] = "user operate"
					workload.Spec.Replicas = &replicas
					workloadName = workload.Namespace + "/" + workload.Name
				}
			}
			if replicas == 0 {
				klog.Infof("Try to suspend workload name=%s", workloadName)
			} else {
				klog.Infof("Try to resume workload name=%s", workloadName)
			}
			err := cli.Update(ctx, w.(client.Object))
			if err != nil {
				klog.Error("Failed to scale workload name=%s err=%v", workloadName, err)
				return err
			}

			klog.Infof("Success to operate workload name=%s", workloadName)
		} // end list object loop

		return nil
	} // end of suspend func

	var deploymentList appsv1.DeploymentList
	err := suspend(&deploymentList)
	if err != nil {
		return err
	}

	var stsList appsv1.StatefulSetList
	err = suspend(&stsList)

	return err
}

func isStartUp(am *appv1alpha1.ApplicationManager, cli client.Client) (bool, error) {
	var labelSelector string
	var deployment appsv1.Deployment

	err := cli.Get(context.TODO(), types.NamespacedName{Name: am.Spec.AppName, Namespace: am.Spec.AppNamespace}, &deployment)

	if err == nil {
		labelSelector = metav1.FormatLabelSelector(deployment.Spec.Selector)
	}

	if apierrors.IsNotFound(err) {
		var sts appsv1.StatefulSet
		err = cli.Get(context.TODO(), types.NamespacedName{Name: am.Spec.AppName, Namespace: am.Spec.AppNamespace}, &sts)
		if err != nil {
			return false, err

		}
		labelSelector = metav1.FormatLabelSelector(sts.Spec.Selector)
	}
	var pods corev1.PodList
	//pods, err := h.client.KubeClient.Kubernetes().CoreV1().Pods(h.app.Namespace).
	//	List(h.ctx, metav1.ListOptions{LabelSelector: labelSelector})
	selector, _ := labels.Parse(labelSelector)
	err = cli.List(context.TODO(), &pods, &client.ListOptions{Namespace: am.Spec.AppNamespace, LabelSelector: selector})
	if len(pods.Items) == 0 {
		return false, errors.New("no pod found..")
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
			return true, nil
		}
	}
	return false, nil
}

func makeRecord(opType appv1alpha1.OpType, source string, version string, status appv1alpha1.ApplicationManagerState, message string) *appv1alpha1.OpRecord {
	now := metav1.Now()
	return &appv1alpha1.OpRecord{
		OpType:    opType,
		Source:    source,
		Version:   version,
		Message:   message,
		Status:    status,
		StateTime: &now,
	}
}
