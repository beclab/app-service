package controllers

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/util/wait"
	"sync"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/images"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
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

// ApplicationManagerController represents a controller for managing the lifecycle of applicationmanager.
type ApplicationManagerController struct {
	client.Client
	KubeConfig            *rest.Config
	ImageClient           images.ImageManager
	runningReconciles     map[string]struct{}
	runningReconcilesLock sync.RWMutex
}

// SetupWithManager sets up the ApplicationManagerController with the provided controller manager
func (r *ApplicationManagerController) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("app-manager-controller", mgr, controller.Options{
		MaxConcurrentReconciles: 1,
		Reconciler:              r,
	})
	if err != nil {
		klog.Errorf("app manager setup failed %v", err)
		return fmt.Errorf("app manager setup failed %w", err)
	}
	r.runningReconciles = make(map[string]struct{})

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
				time.Sleep(time.Second)
				return r.preEnqueueCheckForUpdate(e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// TODO: is there need to uninstall app when delete application manager
				// ?? may be need
				return false
			},
		},
	)

	if err != nil {
		klog.Errorf("add watch for application manager failed %v", err)
		return fmt.Errorf("add watch failed %w", err)
	}

	// start auto reconcile the application manager state
	go wait.Until(r.ReconcileAll, 5*time.Minute, wait.NeverStop)

	return nil
}

func (r *ApplicationManagerController) ReconcileAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var amList appv1alpha1.ApplicationManagerList
	err := r.List(ctx, &amList)
	if err != nil {
		klog.Errorf("list application manager failed %v", err)
		return
	}

	for _, am := range amList.Items {
		//if _, ok := appstate.OperatingStates[am.Status.State]; ok {
		//	continue
		//}
		err = r.reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: am.Name,
			},
		})

		if err != nil {
			klog.Errorf("reconcile application manager %s failed %v", err, ", ", am.Name)
		}
		time.Sleep(time.Second)
	} // end of app mgr list loop
}

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	go func() {
		err := r.reconcile(ctx, req)
		if err != nil {
			klog.Errorf("reconcile err: %v", err)
		}
	}()

	return ctrl.Result{}, nil
}

func (r *ApplicationManagerController) reconcile(ctx context.Context, req ctrl.Request) error {
	r.runningReconcilesLock.Lock()
	_, exists := r.runningReconciles[req.Name]
	r.runningReconcilesLock.Unlock()
	if exists {
		klog.Infof("get lock exists: %v", req.Name)
		return nil
	}

	r.runningReconcilesLock.Lock()
	klog.Infof("set lock exists: %v", req.Name)
	r.runningReconciles[req.Name] = struct{}{}
	r.runningReconcilesLock.Unlock()

	defer func() {
		r.runningReconcilesLock.Lock()
		klog.Infof("delete lock %v", req.Name)
		delete(r.runningReconciles, req.Name)
		r.runningReconcilesLock.Unlock()
	}()

	var am appv1alpha1.ApplicationManager
	e := r.Get(ctx, types.NamespacedName{Name: req.Name}, &am)
	if e != nil {
		return e
	}
	klog.Infof("reconcile application manager request name=%s:%s", req.Name, am.Status.State)

	pctx, cancelFunc := context.WithTimeout(ctx, appstate.StateToDuration(am.Status.State))
	defer func() {
		klog.Infof("execute cancelFunc...:name: %s-%s", am.Name, am.Status.State)
		cancelFunc()
	}()
	klog.Infof("reconcile app %s state: %v", am.Spec.AppName, am.Status.State)
	if appstate.IsCancelable(am.Status.State) {
		klog.Infof("store cancel func: %s-%s", req.Name, am.Status.State)
		appstate.StoreCancelFunc(req.Name, cancelFunc)
		defer func() {
			klog.Infof("delete cancel func: %s-%s", req.Name, am.Status.State)
			appstate.DelCancelFunc(req.Name)
		}()
	}

	statefulApp, err := LoadStatefulApp(r, req.Name)
	klog.Infof("reconcile stateful app :%v", req.Name)
	if err != nil {
		klog.Errorf("load stateful app failed %v", err)
		if errors.Is(err, appstate.ErrUnknownState) {
			updateErr := r.updateStatus(ctx, &am, appv1alpha1.Stopping)
			if updateErr != nil {
				klog.Errorf("update app manager %s to %s state failed", req.Name, appv1alpha1.Stopping, updateErr)
				return updateErr
			}
		}

		return err
	}

	if statefulApp == nil {
		// app not found
		klog.Warning("reconciling app not found, ", req.Name)
		return nil
	}

	rChan := make(chan error)
	doneChan := make(chan struct{})
	defer close(doneChan)
	go statefulApp.Exec(pctx, rChan)
	go statefulApp.HandleContext(pctx, rChan, doneChan)

	ret := <-rChan
	klog.Infof("reconciled application manager request name=%s state=%s", req.Name, statefulApp.State())

	return ret
}

func (r *ApplicationManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	am, _ := obj.(*appv1alpha1.ApplicationManager)
	if am.Status.State == "" {
		return false
	}
	return true
}

func (r *ApplicationManagerController) handleCancel(am *appv1alpha1.ApplicationManager) error {
	klog.Infof("start to perform cancel operation app=%s", am.Spec.AppName)
	ctx := context.Background()
	statefulApp, err := LoadStatefulApp(r, am.Name)
	if err != nil {
		klog.Errorf("load stateful app failed in handle cancel %v", err)
		return err
	}
	if statefulApp == nil {
		// app not found
		klog.Warningf("reconciling app %s not found", am.Name)
		return nil
	}
	rChan := make(chan error)
	statefulApp.Exec(ctx, rChan)
	result := <-rChan
	return result
}

func (r *ApplicationManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	oldAppMgr, _ := old.(*appv1alpha1.ApplicationManager)
	curAppMgr, _ := new.(*appv1alpha1.ApplicationManager)

	if curAppMgr.Spec.Type != appv1alpha1.App {
		return false
	}
	if curAppMgr.Status.OpGeneration <= oldAppMgr.Status.OpGeneration {
		return false
	}

	if appstate.IsCanceling(curAppMgr.Status.State) {
		go r.handleCancel(curAppMgr)
		return false
	}

	return true
}

func (r *ApplicationManagerController) updateStatus(ctx context.Context, am *appv1alpha1.ApplicationManager, state appv1alpha1.ApplicationManagerState) error {
	err := r.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		klog.Errorf("get app manager %s failed %v", am.Name, err)
		return nil
	}
	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	amCopy.Status.OpGeneration += 1
	err = r.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		klog.Errorf("update app manager %s status failed %v", am.Name)
		return err
	}
	return nil
}
