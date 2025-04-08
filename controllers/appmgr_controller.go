package controllers

import (
	"bytetrade.io/web3os/app-service/pkg/apiserver"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"k8s.io/apimachinery/pkg/labels"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const suspendAnnotation = "bytetrade.io/suspend-by"
const suspendCauseAnnotation = "bytetrade.io/suspend-cause"

var appManagerNotFound = &apierrors.StatusError{ErrStatus: metav1.Status{Code: http.StatusNotFound}}

var manager map[string]context.CancelFunc

// ApplicationManagerController represents a controller for managing the lifecycle of applicationmanager.
type ApplicationManagerController struct {
	client.Client
	KubeConfig  *rest.Config
	ImageClient images.ImageManager
}

// SetupWithManager sets up the ApplicationManagerController with the provided controller manager
func (r *ApplicationManagerController) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("app-manager-controller", mgr, controller.Options{
		MaxConcurrentReconciles: 1,
		Reconciler:              r,
	})
	if err != nil {
		return fmt.Errorf("app manager setup failed %w", err)
	}

	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.ApplicationManager{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name: h.GetName(),
				}}}
			}),
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.preEnqueueCheckForCreate(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.preEnqueueCheckForUpdate(e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// TODO:hysyeah
				// is there need to uninstall app when delete application manager
				// ?? may be need
				return false
			},
		},
	)

	if err != nil {
		return fmt.Errorf("add watch failed %w", err)
	}

	return nil
}

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("reconcile application manager request name=%s", req.Name)
	taskName, err := r.getNextTask(ctx)

	klog.Infof("taskName: %s", taskName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.Errorf("get next task error %v", err)
		return ctrl.Result{}, err
	}

	var am appv1alpha1.ApplicationManager
	err = r.Get(ctx, types.NamespacedName{Name: taskName}, &am)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if am.Status.Completed {
		return ctrl.Result{}, nil
	}

	klog.Infof("am.Status.State: %s, opTime: %v", am.Status.State, am.Status.OpTime)
	if am.Status.State == "" || am.Status.State == appv1alpha1.Pending {
		am.Status.State = appv1alpha1.Downloading
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		err = r.Status().Update(context.TODO(), &am)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	return r.reconcile(&am)

}

func (r *ApplicationManagerController) reconcile(am *appv1alpha1.ApplicationManager) (ctrl.Result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error

	defer func() {
		// TODO:hysyeah
		// add opType record
		// install, uninstall, cancel, upgrade
		klog.Infof("reconcile finished err=%v", err)
	}()
	if manager == nil {
		manager = make(map[string]context.CancelFunc)
	}
	manager[am.Name] = cancel

	switch am.Status.State {
	case appv1alpha1.Downloading:
		klog.Infof("start to download app %s image", am.Spec.AppName)
		//err = r.handleDownload(ctx, am)
		err = handleWithContext(ctx, am, r.handleDownload)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("download failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.Installing:
		klog.Infof("start to install app %s", am.Spec.AppName)
		err = r.handleInstall(ctx, am)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("install failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.Initializing:
		klog.Infof("start to initial app %s", am.Spec.AppName)
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Hour)
		manager[am.Name] = timeoutCancel
		defer timeoutCancel()
		err = r.handleInitial(timeoutCtx, am)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("initial failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.Suspending:
		// TODO: add check for state transfer
		// only initializing and running can transfer to suspending
		// TODO:hysyeah
		klog.Infof("start to suspend app %s", am.Spec.AppName)
		err = r.handleSuspend(ctx, am)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("suspend failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.Upgrading:
		// TODO:hysyeah
		klog.Infof("start upgrading")
	case appv1alpha1.Canceling:

		return ctrl.Result{}, nil

	case appv1alpha1.Resuming:
		klog.Infof("start to resume app %s", am.Spec.AppName)
		err = r.handleResume(ctx, am)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("resume failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.Uninstalling:
		klog.Infof("start to uninstall app %s", am.Spec.AppName)
		err = r.handleUninstall(ctx, am)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("uninstall failed %w", err)
		}
		return ctrl.Result{}, nil
	case appv1alpha1.PendingCanceled, appv1alpha1.DownloadingCanceled, appv1alpha1.InitializingCanceled,
		appv1alpha1.InstallingCanceled, appv1alpha1.UpgradingCanceled, appv1alpha1.ResumingCanceled,
		appv1alpha1.Running, appv1alpha1.Uninstalled, appv1alpha1.Suspended,
		appv1alpha1.DownloadFailed, appv1alpha1.InstallFailed, appv1alpha1.InitialFailed,
		appv1alpha1.SuspendFailed, appv1alpha1.CancelFailed, appv1alpha1.UninstallFailed,
		appv1alpha1.UpgradeFailed, appv1alpha1.ResumeFailed:
		if !am.Status.Completed {
			amCopy := am.DeepCopy()
			amCopy.Status.Completed = true
			err = r.Status().Patch(ctx, am, client.MergeFrom(amCopy))
			if err != nil {
				return ctrl.Result{Requeue: false}, err
			}
		}
		return ctrl.Result{Requeue: false}, nil

	}
	return ctrl.Result{}, nil
}

func (r *ApplicationManagerController) reconcile2(cur *appv1alpha1.ApplicationManager) {
	var err error
	klog.Infof("Start to handle operate=%s app=%s", cur.Status.OpType, cur.Spec.AppName)
	klog.Infof("Start to handle cur.Status.State=%s", cur.Status.State)
	switch cur.Status.State {
	case appv1alpha1.Canceling:
		//if cur.Status.State == appv1alpha1.Installing || cur.Status.State == appv1alpha1.Downloading ||
		//	cur.Status.State == appv1alpha1.Initializing || cur.Status.State == appv1alpha1.Upgrading || cur.Status.State == appv1alpha1.Resuming {
		klog.Infof("xxx...reconcile2")
		err = r.cancel(cur)
		//}
		if cur.Status.State == appv1alpha1.Pending {
			err = r.updateStatus(context.TODO(), cur, appv1alpha1.PendingCanceled, nil, constants.OperationCanceledByUserTpl)
			if err != nil {
				klog.Info("Failed to update applicationmanagers status name=%s err=%v", cur.Name, err)
			}
		}
	default:
	}

	if err != nil {
		klog.Errorf("Failed to handle operate=%s app=%s err=%v", cur.Status.OpType, cur.Spec.AppName, err)
	} else {
		klog.Infof("Success to handle operate=%s app=%s", cur.Status.OpType, cur.Spec.AppName)
	}

}

func (r *ApplicationManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	cur, _ := obj.(*appv1alpha1.ApplicationManager)
	if cur.Spec.Type != appv1alpha1.App {
		return false
	}

	state := cur.Status.State

	if state == appv1alpha1.DownloadFailed || state == appv1alpha1.InstallFailed ||
		state == appv1alpha1.InitialFailed || state == appv1alpha1.SuspendFailed ||
		state == appv1alpha1.CancelFailed || state == appv1alpha1.UninstallFailed ||
		state == appv1alpha1.UpgradeFailed || state == appv1alpha1.ResumeFailed ||
		state == appv1alpha1.PendingCanceled || state == appv1alpha1.DownloadingCanceled ||
		state == appv1alpha1.InstallingCanceled || state == appv1alpha1.InitializingCanceled ||
		state == appv1alpha1.UpgradingCanceled || state == appv1alpha1.ResumingCanceled {
		return false
	}

	// for compatibility
	if state == "completed" {
		return false
	}

	if state == appv1alpha1.Canceling {
		go r.reconcile2(cur)
		return false
	}

	return true
}

func (r *ApplicationManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	//oldAppMgr, _ := old.(*appv1alpha1.ApplicationManager)
	curAppMgr, _ := new.(*appv1alpha1.ApplicationManager)
	//opType := curAppMgr.Status.OpType

	if curAppMgr.Spec.Type != appv1alpha1.App {
		return false
	}

	//if curAppMgr.Status.OpGeneration <= oldAppMgr.Status.OpGeneration {
	//	return false
	//}

	if curAppMgr.Status.State == appv1alpha1.Canceling {
		go r.reconcile2(curAppMgr)
		return false
	}

	return true
}

func (r *ApplicationManagerController) updateStatus(ctx context.Context, am *appv1alpha1.ApplicationManager, state appv1alpha1.ApplicationManagerState,
	opRecord *appv1alpha1.OpRecord, message string) error {
	var err error
	appState := ""
	if appv1alpha1.AppStateCollect.Has(am.Status.State.String()) {
		appState = am.Status.State.String()
	}
	err = r.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		return err
	}

	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.Message = message
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	if opRecord != nil {
		amCopy.Status.OpRecords = append([]appv1alpha1.OpRecord{*opRecord}, amCopy.Status.OpRecords...)
	}
	if len(amCopy.Status.OpRecords) > 20 {
		amCopy.Status.OpRecords = amCopy.Status.OpRecords[:20:20]
	}
	err = r.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		return err
	}
	var aa appv1alpha1.ApplicationManager
	err = r.Get(ctx, types.NamespacedName{Name: am.Name}, &aa)
	if err != nil {
		return err
	}
	if len(appState) > 0 {
		err = utils.UpdateAppState(ctx, am, appState)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ApplicationManagerController) rollback(am *appv1alpha1.ApplicationManager) (err error) {
	appConfig := &appinstaller.ApplicationConfig{
		AppName:   am.Spec.AppName,
		Namespace: am.Spec.AppNamespace,
		OwnerName: am.Spec.AppOwner,
	}
	token := am.Status.Payload["token"]
	ops, err := appinstaller.NewHelmOps(context.TODO(), r.KubeConfig, appConfig, token, appinstaller.Opt{})
	if err != nil {
		return err
	}

	err = ops.RollBack()

	return err
}

func (r *ApplicationManagerController) cancel(am *appv1alpha1.ApplicationManager) (err error) {
	cancel, ok := manager[am.Name]
	if !ok {
		return errors.New("can not execute cancel")
	}
	cancel()
	var im appv1alpha1.ImageManager
	err = r.Get(context.TODO(), types.NamespacedName{Name: am.Name}, &im)
	if err != nil {
		return err
	}
	imCopy := im.DeepCopy()
	imCopy.Status.State = "canceled"
	now := metav1.Now()
	imCopy.Status.StatusTime = &now
	imCopy.Status.UpdateTime = &now
	err = r.Status().Patch(context.TODO(), imCopy, client.MergeFrom(&im))
	if err != nil {
		return err
	}
	klog.Infof("cancal...status1: %v", am.Status)

	state := appv1alpha1.Canceling
	if am.Status.State == appv1alpha1.Downloading {
		state = appv1alpha1.DownloadingCanceled
	}
	if am.Status.State == appv1alpha1.Installing {
		state = appv1alpha1.InstallingCanceled
	}
	if am.Status.State == appv1alpha1.Initializing || am.Status.State == appv1alpha1.Upgrading ||
		am.Status.State == appv1alpha1.Resuming {
		state = appv1alpha1.Suspending
	}
	if !ok {
		am.Status.State = appv1alpha1.CancelFailed
	}
	klog.Infof("cancal...status2: %v", am.Status)
	return r.updateStatus(context.TODO(), am, state, nil, am.Status.Message)
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

//func (r *ApplicationManagerController) resumeAppAndWaitForLaunch(appMgr *appv1alpha1.ApplicationManager) (err error) {
//	defer func() {
//		if err != nil {
//			message := fmt.Sprintf(constants.OperationFailedTpl, appMgr.Status.OpType, err.Error())
//			e := r.updateStatus(context.TODO(), appMgr, appv1alpha1.Failed, nil, "", message)
//			if e != nil {
//				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
//			}
//		}
//	}()
//	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
//	defer cancel()
//	err = suspendOrResumeApp(ctx, r.Client, appMgr, int32(1))
//	if err != nil {
//		return err
//	}
//	err = r.updateStatus(ctx, appMgr, appv1alpha1.Resuming, nil, appv1alpha1.AppResuming, appv1alpha1.AppResuming.String())
//	if err != nil {
//		klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, err)
//		return err
//	}
//
//	var apps appv1alpha1.ApplicationList
//	err = r.List(ctx, &apps)
//	if err != nil {
//		return err
//	}
//	var app appv1alpha1.Application
//	listObjects, err := apimeta.ExtractList(&apps)
//
//	for _, obj := range listObjects {
//		a, _ := obj.(*appv1alpha1.Application)
//		if a.Spec.Namespace == appMgr.Spec.AppNamespace {
//			app = *a
//		}
//	}
//	timer := time.NewTicker(2 * time.Second)
//	entrances := app.Spec.Entrances
//	entranceCount := len(entrances)
//	for {
//		select {
//		case <-timer.C:
//			count := 0
//			for _, e := range entrances {
//				klog.Infof("Waiting for service launch host=%s", e.Host)
//				host := fmt.Sprintf("%s.%s", e.Host, app.Spec.Namespace)
//				if utils.TryConnect(host, strconv.Itoa(int(e.Port))) {
//					count++
//				}
//			}
//			if entranceCount == count {
//				message := fmt.Sprintf(constants.ResumeOperationCompletedTpl, appMgr.Spec.AppName)
//				e := r.updateStatus(ctx, appMgr, appv1alpha1.Completed, nil, appv1alpha1.AppRunning, message)
//				return e
//			}
//
//		case <-ctx.Done():
//			err = errors.New("wait for resume app to running failed: context canceled")
//			e := suspendOrResumeApp(context.TODO(), r.Client, appMgr, 0)
//			if e != nil {
//				klog.Errorf("rollback to suspend err=%v", err)
//			}
//			e = r.updateStatus(context.TODO(), appMgr, appv1alpha1.Completed, nil, appv1alpha1.AppSuspend, "resume failed rollback")
//			if e != nil {
//				klog.Errorf("rollback to suspend, update status err=%v", err)
//			}
//			return err
//		}
//	}
//}

func (r *ApplicationManagerController) pollDownloadProgress(ctx context.Context, am *appv1alpha1.ApplicationManager) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var im appv1alpha1.ImageManager
			err := r.Get(ctx, types.NamespacedName{Name: am.Name}, &im)
			if err != nil {
				klog.Infof("Failed to get imanagermanagers name=%s err=%v", am.Name, err)
				return err
			}

			if im.Status.State == "failed" {
				return errors.New(im.Status.Message)
			}

			type progress struct {
				offset int64
				total  int64
			}
			maxImageSize := int64(50173680066)

			nodeMap := make(map[string]*progress)
			for _, nodeName := range im.Spec.Nodes {
				for _, ref := range im.Spec.Refs {
					t := im.Status.Conditions[nodeName][ref.Name]["total"]
					if t == "" {
						t = strconv.FormatInt(maxImageSize, 10)
					}

					total, _ := strconv.ParseInt(t, 10, 64)

					t = im.Status.Conditions[nodeName][ref.Name]["offset"]
					if t == "" {
						t = "0"
					}
					offset, _ := strconv.ParseInt(t, 10, 64)

					if _, ok := nodeMap[nodeName]; ok {
						nodeMap[nodeName].offset += offset
						nodeMap[nodeName].total += total
					} else {
						nodeMap[nodeName] = &progress{offset: offset, total: total}
					}
				}
			}
			ret := math.MaxFloat64
			for _, p := range nodeMap {
				var nodeProgress float64
				if p.total != 0 {
					nodeProgress = float64(p.offset) / float64(p.total)
				}
				if nodeProgress < ret {
					ret = nodeProgress * 100
				}

			}

			var cur appv1alpha1.ApplicationManager
			err = r.Get(ctx, types.NamespacedName{Name: am.Name}, &cur)
			if err != nil {
				klog.Infof("Failed to get applicationmanagers name=%v, err=%v", am.Name, err)
				continue
			}

			appMgrCopy := cur.DeepCopy()
			oldProgress, _ := strconv.ParseFloat(cur.Status.Progress, 64)
			if oldProgress > ret {
				continue
			}
			cur.Status.Progress = strconv.FormatFloat(ret, 'f', 2, 64)
			err = r.Status().Patch(ctx, &cur, client.MergeFrom(appMgrCopy))
			if err != nil {
				klog.Infof("Failed to patch applicationmanagers name=%v, err=%v", am.Name, err)
				continue
			}

			klog.Infof("download progress.... %v", cur.Status.Progress)
			if cur.Status.Progress == "100.00" {
				return nil
			}
		case <-ctx.Done():
			return context.Canceled
		}
	}
}

func (r *ApplicationManagerController) createImageManager(ctx context.Context, appMgr *appv1alpha1.ApplicationManager, refs []appv1alpha1.Ref) error {
	var nodes corev1.NodeList
	err := r.List(ctx, &nodes, &client.ListOptions{})
	if err != nil {
		klog.Infof("Failed to list err=%v", err)
		return err
	}

	nodeList := make([]string, 0)
	for _, node := range nodes.Items {
		if !utils.IsNodeReady(&node) || node.Spec.Unschedulable {
			continue
		}
		nodeList = append(nodeList, node.Name)
	}
	if len(nodeList) == 0 {
		return errors.New("cluster has no suitable node to schedule")
	}

	var im appv1alpha1.ImageManager
	err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &im)
	if err == nil {
		err = r.Delete(ctx, &im)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	labels := make(map[string]string)
	if strings.HasSuffix(appMgr.Spec.AppName, "-dev") {
		labels["dev.bytetrade.io/dev-owner"] = appMgr.Spec.AppOwner
	}
	m := appv1alpha1.ImageManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:   appMgr.Name,
			Labels: labels,
		},
		Spec: appv1alpha1.ImageManagerSpec{
			AppName:      appMgr.Spec.AppName,
			AppNamespace: appMgr.Spec.AppNamespace,
			AppOwner:     appMgr.Spec.AppOwner,
			Refs:         refs,
			Nodes:        nodeList,
		},
	}
	err = r.Create(ctx, &m)
	if err != nil {
		return err
	}
	return nil
}

type portKey struct {
	port     int32
	protocol string
}

func setExposePorts(ctx context.Context, appConfig *appinstaller.ApplicationConfig) error {
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

func (r *ApplicationManagerController) getNextTask(ctx context.Context) (string, error) {
	var appManagerList appv1alpha1.ApplicationManagerList
	err := r.List(ctx, &appManagerList)
	if err != nil {
		klog.Errorf("list application manager failed %v", err)
		return "", err
	}
	filteredAppManagers := make([]appv1alpha1.ApplicationManager, 0)
	for _, am := range appManagerList.Items {
		if am.Spec.Type != appv1alpha1.App {
			continue
		}
		if am.Status.Completed {
			continue
		}
		// TODO:hysyeah  for compatibility
		if am.Status.State == "completed" || am.Status.State == "canceled" || am.Status.State == "failed" {
			continue
		}
		if am.Status.OpTime.IsZero() {
			continue
		}

		filteredAppManagers = append(filteredAppManagers, am)
	}
	sort.Slice(filteredAppManagers, func(i, j int) bool {
		return filteredAppManagers[i].Status.OpTime.Before(filteredAppManagers[j].Status.OpTime)
	})
	if len(filteredAppManagers) == 0 {
		return "", appManagerNotFound
	}
	return filteredAppManagers[0].Name, nil
}

func (r *ApplicationManagerController) handleDownload(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		//originState := am.Status.State
		am.Status.State = appv1alpha1.Installing
		am.Status.Message = "image downloaded"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now

		if err != nil {
			am.Status.State = appv1alpha1.DownloadFailed
			am.Status.Message = fmt.Sprintf("downloading phase failed %v", err)
		}
		if errors.Is(err, context.Canceled) {
			klog.Infof("downloading canceled")
			am.Status.State = appv1alpha1.DownloadingCanceled
		}
		//if updateErr := r.Status().Update(context.TODO(), am); updateErr != nil {
		//	err = updateErr
		//	klog.Errorf("failed to update am %s state from %s to %s, err=%v", am.Name, originState, am.Status.State, updateErr)
		//}

		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()

	var appCfg *appinstaller.ApplicationConfig
	err = json.Unmarshal([]byte(am.Spec.Config), &appCfg)
	if err != nil {
		klog.Errorf("Failed to parse application config %v", err)
		return err
	}
	admin, err := kubesphere.GetAdminUsername(ctx, r.KubeConfig)
	values := map[string]interface{}{
		"admin": admin,
		"bfl": map[string]string{
			"username": am.Spec.AppOwner,
		},
	}

	imageRefs, err := utils.GetRefFromResourceList(appCfg.ChartsName, values)
	if err != nil {
		klog.Errorf("Failed to get image refs %v", err)
		return err
	}

	err = r.ImageClient.Create(ctx, am, imageRefs)
	if err != nil {
		klog.Errorf("Failed to create image manager %v", err)
		return err
	}
	err = r.ImageClient.PollDownloadProgress(ctx, am)
	if err != nil {
		klog.Errorf("Failed to poll download progress %v", err)
		return err
	}
	return nil
}

func (r *ApplicationManagerController) handleInstall(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		am.Status.State = appv1alpha1.Initializing
		am.Status.Message = "app deployed and container startup"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now

		if err != nil {
			am.Status.State = appv1alpha1.InstallFailed
			am.Status.Message = fmt.Sprintf("installing phase failed %v", err)
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()

	payload := am.Status.Payload
	token := payload["token"]

	var appCfg *appinstaller.ApplicationConfig
	err = json.Unmarshal([]byte(am.Spec.Config), &appCfg)
	if err != nil {
		return err
	}
	ops, err := appinstaller.NewHelmOps(ctx, r.KubeConfig, appCfg, token, appinstaller.Opt{Source: am.Spec.Source})
	if err != nil {
		return err
	}
	err = setExposePorts(ctx, appCfg)
	if err != nil {
		return err
	}
	err = ops.Install2()
	if err != nil {
		return err
	}
	return nil
}

func (r *ApplicationManagerController) handleInitial(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		am.Status.State = appv1alpha1.Running
		am.Status.Message = "initial success"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		if err != nil {
			am.Status.State = appv1alpha1.InitialFailed
			am.Status.Message = fmt.Sprintf("initializing phase failed %v", err)
			// if user canceled the op and state is equal initializing, turn state to suspending
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				am.Status.State = appv1alpha1.Suspending
			}
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()

	payload := am.Status.Payload
	token := payload["token"]

	var appCfg *appinstaller.ApplicationConfig
	err = json.Unmarshal([]byte(am.Spec.Config), &appCfg)
	if err != nil {
		return err
	}
	ops, err := appinstaller.NewHelmOps(ctx, r.KubeConfig, appCfg, token,
		appinstaller.Opt{Source: am.Spec.Source})
	if err != nil {
		return err
	}
	ok, err := ops.WaitForLaunch()
	if !ok {
		return err
	}
	return nil
}

func (r *ApplicationManagerController) handleSuspend(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {

	defer func() {
		am.Status.State = appv1alpha1.Suspended
		am.Status.Message = "app suspend success"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		if err != nil {
			am.Status.State = appv1alpha1.SuspendFailed
			am.Status.Message = fmt.Sprintf("suspend app %s failed %v", am.Spec.AppName, err)
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()
	err = suspendOrResumeApp(ctx, r.Client, am, int32(0))
	if err != nil {
		return fmt.Errorf("suspend app %s failed %w", am.Spec.AppName, err)
	}
	return nil
}

func (r *ApplicationManagerController) handleResume(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		am.Status.State = appv1alpha1.Initializing
		am.Status.Message = "from resuming to initializing"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		if err != nil {
			am.Status.State = appv1alpha1.SuspendFailed
			am.Status.Message = fmt.Sprintf("resume app %s failed %v", am.Spec.AppName, err)
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()
	err = suspendOrResumeApp(ctx, r.Client, am, int32(1))
	if err != nil {
		return fmt.Errorf("resume app %s failed %w", am.Spec.AppOwner, err)
	}
	ok := r.IsStartUp(am)
	if ok {
		//TODO:hysyeah
		// if startup success turn to initilizing state
		// if startup failed to failed
		// if timeout ->> ???
		return nil
	}
	return errors.New("startup failed")
}

func (r *ApplicationManagerController) handleUninstall(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		am.Status.State = appv1alpha1.Uninstalled
		am.Status.Message = "app uninstalled success"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		if err != nil {
			am.Status.State = appv1alpha1.UninstallFailed
			am.Status.Message = fmt.Sprintf("uninstall app %s failed %v", am.Spec.AppName, err)
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()

	token := am.Status.Payload["token"]
	appCfg := &appinstaller.ApplicationConfig{
		AppName:   am.Spec.AppName,
		Namespace: am.Spec.AppNamespace,
		OwnerName: am.Spec.AppOwner,
	}

	ops, err := appinstaller.NewHelmOps(ctx, r.KubeConfig, appCfg, token, appinstaller.Opt{})
	if err != nil {
		return err
	}
	err = ops.Uninstall()
	if err != nil {
		return err
	}
	return nil
}

func (r *ApplicationManagerController) isStartUp(am *appv1alpha1.ApplicationManager) (bool, error) {
	var labelSelector string
	var deployment appsv1.Deployment

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: am.Spec.AppName, Namespace: am.Spec.AppNamespace}, &deployment)

	if err == nil {
		labelSelector = metav1.FormatLabelSelector(deployment.Spec.Selector)
	}

	if apierrors.IsNotFound(err) {
		var sts appsv1.StatefulSet
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: am.Spec.AppName, Namespace: am.Spec.AppNamespace}, &sts)
		if err != nil {
			return false, err

		}
		labelSelector = metav1.FormatLabelSelector(sts.Spec.Selector)
	}
	var pods corev1.PodList
	//pods, err := h.client.KubeClient.Kubernetes().CoreV1().Pods(h.app.Namespace).
	//	List(h.ctx, metav1.ListOptions{LabelSelector: labelSelector})
	selector, _ := labels.Parse(labelSelector)
	err = r.Client.List(context.TODO(), &pods, &client.ListOptions{Namespace: am.Spec.AppNamespace, LabelSelector: selector})
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

func (r *ApplicationManagerController) IsStartUp(am *appv1alpha1.ApplicationManager) bool {
	timer := time.NewTicker(1 * time.Second)

	for {
		select {
		case <-timer.C:
			startedUp, _ := r.isStartUp(am)
			if startedUp {
				klog.Infof("time: %v, appState: %v", time.Now(), appv1alpha1.AppInitializing)
				return true
			}

			//case <-h.ctx.Done():
			//	klog.Infof("Waiting for app startup canceled appName=%s", h.app.AppName)
			//	return false
		}
	}
}

func (r *ApplicationManagerController) handleUpgrade(ctx context.Context, am *appv1alpha1.ApplicationManager) (err error) {
	var version string
	var actionConfig *action.Configuration

	actionConfig, _, err = helm.InitConfig(r.KubeConfig, am.Spec.AppNamespace)
	if err != nil {
		return err
	}

	defer func() {
		am.Status.State = appv1alpha1.Initializing
		am.Status.Message = "app enter initializing from upgrading"
		now := metav1.Now()
		am.Status.StatusTime = &now
		am.Status.UpdateTime = &now
		if err != nil {
			am.Status.State = appv1alpha1.UpgradeFailed
			am.Status.Message = fmt.Sprintf("upgrade app %s failed %v", am.Spec.AppName, err)
		}
		updateErr := r.updateStatus(ctx, am, am.Status.State, nil, am.Status.Message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		return
	}()
	var appConfig *appinstaller.ApplicationConfig

	deployedVersion, _, err := utils.GetDeployedReleaseVersion(actionConfig, am.Spec.AppName)
	if err != nil {
		klog.Errorf("Failed to get release revision err=%v", err)
	}

	if !utils.MatchVersion(version, ">= "+deployedVersion) {
		return errors.New("upgrade version should great than deployed version")
	}

	version = am.Status.Payload["version"]
	cfgURL := am.Status.Payload["cfgURL"]
	repoURL := am.Status.Payload["repoURL"]
	token := am.Status.Payload["token"]
	var chartPath string

	admin, err := kubesphere.GetAdminUsername(ctx, r.KubeConfig)
	if err != nil {
		return err
	}
	if !userspace.IsSysApp(am.Spec.AppName) {
		appConfig, chartPath, err = apiserver.GetAppConfig(ctx, am.Spec.AppName, am.Spec.AppOwner, cfgURL, repoURL, version, token, admin)
		if err != nil {
			return err
		}
	} else {
		chartPath, err = apiserver.GetIndexAndDownloadChart(ctx, am.Spec.AppName, repoURL, version, token)
		if err != nil {
			return err
		}
		appConfig = &appinstaller.ApplicationConfig{
			AppName:    am.Spec.AppName,
			Namespace:  am.Spec.AppNamespace,
			OwnerName:  am.Spec.AppOwner,
			ChartsName: chartPath,
			RepoURL:    repoURL,
		}
	}
	ops, err := appinstaller.NewHelmOps(ctx, r.KubeConfig, appConfig, token, appinstaller.Opt{Source: am.Spec.Source})
	if err != nil {
		return err
	}
	values := map[string]interface{}{
		"admin": admin,
		"bfl": map[string]string{
			"username": am.Spec.AppOwner,
		},
	}
	refs, err := utils.GetRefFromResourceList(chartPath, values)
	if err != nil {
		return err
	}
	err = r.ImageClient.Create(ctx, am, refs)
	if err != nil {
		return err
	}
	err = r.ImageClient.PollDownloadProgress(ctx, am)
	if err != nil {
		return err
	}
	err = ops.Upgrade()
	if err != nil {
		return err
	}

	return nil
}

func handleWithContext(ctx context.Context, am *appv1alpha1.ApplicationManager, fn func(context.Context, *appv1alpha1.ApplicationManager) error) (err error) {
	select {
	case <-ctx.Done():
		return
	default:

	}
	func() {
		defer runtime.HandleCrash()
		err = fn(ctx, am)
	}()
	select {
	case <-ctx.Done():
		return
	default:

	}
	return
}
