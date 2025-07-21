package controllers

import (
	"context"
	"fmt"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	deployment  = "Deployment"
	statefulSet = "StatefulSet"
	replicaSet  = "ReplicaSet"
)

type ReasonedMessage struct {
	Reason  string
	Message string
}

// EntranceStatusManagerController manages the status of app entrance
type EntranceStatusManagerController struct {
	client.Client
}

func (r *EntranceStatusManagerController) SetUpWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("entrance-status-manager-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}
	err = c.Watch(
		&source.Kind{Type: &corev1.Pod{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				pod, ok := h.(*corev1.Pod)
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}}}
			}),
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.preEnqueueCheckForUpdate(e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
		},
	)
	if err != nil {
		klog.Errorf("entrance-status-manager-controller failed to watch err=%v", err)
		return err
	}
	return nil
}

func (r *EntranceStatusManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	klog.Infof("reconcile entrance-status-manager request name=%v", req.Name)
	var pod corev1.Pod
	err := r.Get(ctx, req.NamespacedName, &pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	err = r.updateEntranceStatus(&pod)
	if err != nil {
		klog.Errorf("update entrance status err=%v", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *EntranceStatusManagerController) preEnqueueCheckForUpdate(_, new client.Object) bool {
	pod, _ := new.(*corev1.Pod)
	if _, ok := pod.Labels["io.bytetrade.app"]; ok {
		klog.Infof("Pod.Name=%v, olares-app=%v", pod.Name, pod.Labels["io.bytetrade.app"])
		return true
	}
	return false
}

func (r *EntranceStatusManagerController) getStsOrDeploymentReplicasByPod(pod *corev1.Pod) (replicas int32, labelSelector *metav1.LabelSelector, err error) {
	replicas = 1
	if len(pod.OwnerReferences) == 0 {
		return replicas, nil, nil
	}
	var kind, name string
	ownerRef := pod.OwnerReferences[0]
	switch ownerRef.Kind {
	case replicaSet:
		key := types.NamespacedName{Namespace: pod.Namespace, Name: ownerRef.Name}
		var rs appsv1.ReplicaSet
		err = r.Get(context.TODO(), key, &rs)
		if err != nil {
			return replicas, nil, err
		}
		if len(rs.OwnerReferences) > 0 && rs.OwnerReferences[0].Kind == deployment {
			kind = deployment
			name = rs.OwnerReferences[0].Name
		}
	case statefulSet:
		kind = statefulSet
		name = ownerRef.Name
	}
	if kind == "" {
		return replicas, nil, nil
	}
	switch kind {
	case deployment:
		var deploy appsv1.Deployment
		key := types.NamespacedName{Name: name, Namespace: pod.Namespace}
		err = r.Get(context.TODO(), key, &deploy)
		if err != nil {
			return replicas, nil, err
		}
		deployCopy := deploy.DeepCopy()
		labelSelector = deploy.Spec.Selector
		replicas = *deployCopy.Spec.Replicas
	case statefulSet:
		var sts appsv1.StatefulSet
		key := types.NamespacedName{Name: name, Namespace: pod.Namespace}
		err = r.Get(context.TODO(), key, &sts)
		if err != nil {
			return replicas, nil, err
		}
		stsCopy := sts.DeepCopy()
		labelSelector = sts.Spec.Selector
		replicas = *stsCopy.Spec.Replicas

	}
	return replicas, labelSelector, nil
}

func (r *EntranceStatusManagerController) updateEntranceStatus(pod *corev1.Pod) error {
	namespace := pod.Namespace
	var apps v1alpha1.ApplicationList
	err := r.List(context.TODO(), &apps)
	if err != nil {
		return err
	}
	type appInfo struct {
		name         string
		startedTime  *metav1.Time
		entranceName string
	}
	filteredApp := make([]appInfo, 0)

	for _, a := range apps.Items {
		if a.Spec.Namespace != namespace {
			continue
		}
		for _, e := range a.Spec.Entrances {
			isSelected, err := r.isEntrancePod(pod, e.Host, namespace)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if isSelected {
				filteredApp = append(filteredApp, appInfo{
					name:         a.Name,
					startedTime:  a.Status.StartedTime,
					entranceName: e.Name,
				})

			}
		}
	}

	for _, a := range filteredApp {
		//if a.startedTime.IsZero() {
		//	continue
		//}
		var selectedApp v1alpha1.Application
		err = r.Get(context.TODO(), types.NamespacedName{Name: a.name}, &selectedApp)
		if err != nil {
			return err
		}
		appCopy := selectedApp.DeepCopy()
		entranceState, rm, err := r.calEntranceState(pod)
		if err != nil {
			return err
		}
		for i := len(appCopy.Status.EntranceStatuses) - 1; i >= 0; i-- {
			if appCopy.Status.EntranceStatuses[i].Name == a.entranceName {

				appCopy.Status.EntranceStatuses[i].State = entranceState
				appCopy.Status.EntranceStatuses[i].Reason = rm.Reason
				appCopy.Status.EntranceStatuses[i].Message = rm.Message
				now := metav1.Now()
				appCopy.Status.EntranceStatuses[i].StatusTime = &now
			}
		}

		patchApp := client.MergeFrom(&selectedApp)
		err = r.Status().Patch(context.TODO(), appCopy, patchApp)
		klog.Infof("updateEntrances ...:name: %v", appCopy.Name)

		if err != nil {
			klog.Errorf("failed to patch err=%v", err)
			return err
		}
		var am v1alpha1.ApplicationManager
		err = r.Get(context.TODO(), types.NamespacedName{Name: selectedApp.Name}, &am)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("failed to get am name=%s, err=%v", selectedApp.Name, err)
			return err
		}
		if err == nil {
			utils.PublishAsync(appCopy.Spec.Owner, appCopy.Spec.Name, "", "", am.Status.State.String(), "", appCopy.Status.EntranceStatuses)
		}
	}
	return nil
}

func (r *EntranceStatusManagerController) isEntrancePod(pod *corev1.Pod, svcName, namespace string) (bool, error) {
	var svc corev1.Service
	key := types.NamespacedName{Namespace: namespace, Name: svcName}
	err := r.Get(context.TODO(), key, &svc)
	if err != nil {
		return false, err
	}
	selector, err := labels.ValidatedSelectorFromSet(svc.Spec.Selector)
	if err != nil {
		return false, err
	}
	isSelected := selector.Matches(labels.Set(pod.GetLabels()))
	return isSelected, nil
}

func (r *EntranceStatusManagerController) calEntranceState(pod *corev1.Pod) (v1alpha1.EntranceState, ReasonedMessage, error) {
	var message string
	reason := string(pod.Status.Phase)

	replicas, labelSelector, err := r.getStsOrDeploymentReplicasByPod(pod)
	if err != nil {
		klog.Error("get sts or deployment replicas error, ", err, ", ", pod.Namespace, "/", pod.Name)
		return "", ReasonedMessage{Reason: reason}, err
	}
	if replicas == 0 {
		reason = "stopped"
		return v1alpha1.EntranceStopped, ReasonedMessage{
			Reason: reason,
		}, nil
	}
	var state v1alpha1.EntranceState
	if labelSelector == nil {
		state, reason, message = makeEntranceState(pod)
		return state, ReasonedMessage{Reason: reason, Message: message}, nil
	}

	var podList corev1.PodList
	err = r.List(context.TODO(), &podList, client.InNamespace(pod.Namespace), client.MatchingLabels(labelSelector.MatchLabels))
	if err != nil {
		klog.Error("failed to list pods, err=", err, ", ", pod.Namespace, ", ", labelSelector.MatchLabels)
		return state, ReasonedMessage{}, err
	}

	for _, pod := range podList.Items {
		state, reason, message = makeEntranceState(&pod)
		if state == v1alpha1.EntranceRunning {
			return state, ReasonedMessage{
				Reason:  reason,
				Message: message,
			}, nil
		}
	}
	return state, ReasonedMessage{
		Reason:  reason,
		Message: message,
	}, nil
}

func makeEntranceState(pod *corev1.Pod) (v1alpha1.EntranceState, string, string) {
	var reason, message string
	reason = string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}
	if pod.Status.Message != "" {
		message = pod.Status.Message
	}
	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
			continue
		case container.State.Terminated != nil:
			if len(container.State.Terminated.Reason) == 0 {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			if container.State.Terminated.Message != "" {
				message = container.State.Terminated.Message
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + container.State.Waiting.Reason
			if container.State.Waiting.Message != "" {
				message = container.State.Waiting.Message
			}
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}
	totalContainers := len(pod.Spec.Containers)
	readyContainers := 0

	if !initializing {
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]

			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				reason = container.State.Waiting.Reason
				if container.State.Waiting.Message != "" {
					message = container.State.Waiting.Message
				}
			} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				reason = container.State.Terminated.Reason
				if container.State.Terminated.Message != "" {
					message = container.State.Terminated.Message
				}
			} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
				}
				if container.State.Terminated.Message != "" {
					message = container.State.Terminated.Message
				}
			} else if container.Ready && container.State.Running != nil {
				readyContainers++
			}
		}
	}
	if readyContainers == totalContainers && readyContainers != 0 {
		return v1alpha1.EntranceRunning, reason, message
	}

	return v1alpha1.EntranceNotReady, reason, message
}
