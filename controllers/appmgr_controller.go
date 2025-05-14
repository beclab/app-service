package controllers

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/images"
	"k8s.io/client-go/rest"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	KubeConfig            *rest.Config
	ImageClient           images.ImageManager
	tokens                chan struct{}
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
		return fmt.Errorf("app manager setup failed %w", err)
	}
	r.tokens = make(chan struct{}, 5)
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

	// start auto reconcile the application manager state
	//go wait.Until(r.ReconcileAll, 5*time.Second, wait.NeverStop)

	return nil
}

func (r *ApplicationManagerController) ReconcileAll() {
	ctx, cancal := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancal()

	var appManagerList appv1alpha1.ApplicationManagerList
	err := r.List(ctx, &appManagerList)
	if err != nil {
		klog.Errorf("list application manager failed %v", err)
		return
	}

	for _, appmgr := range appManagerList.Items {
		//err = r.reconcile(ctx, ctrl.Request{
		//	NamespacedName: types.NamespacedName{
		//		Name: appmgr.Name,
		//	},
		//})

		if err != nil {
			klog.Error("reconcile application manager error, ", err, ", ", appmgr.Name)
		}
	} // end of app mgr list loop
}

func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	err := r.reconcile(ctx, req)
	if err != nil {
		klog.Errorf("reconcile err: %v", err)
	}
	return ctrl.Result{}, nil
}

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) reconcile(ctx context.Context, req ctrl.Request) error {
	klog.Infof("reconcile application manager request name=%s", req.Name)
	pctx, cancelFunc := context.WithTimeout(ctx, time.Hour)
	defer cancelFunc()
	var am appv1alpha1.ApplicationManager
	e := r.Get(ctx, types.NamespacedName{Name: req.Name}, &am)
	if e != nil {
		return e
	}
	klog.Infof("reconcile....state: %v", am.Status.State)
	if appstate.IsCancelable(am.Status.State) {
		appstate.StoreCancelFunc(req.Name, cancelFunc)
		defer func() {
			appstate.DelCancelFunc(req.Name)
		}()
	}

	statefulApp, err := LoadStatefulApp(pctx, r, req.Name)
	klog.Infof("reconcile stateful app :%v", statefulApp)
	if err != nil {
		klog.Errorf("load stateful app failed %v", err)
		if appstate.IsUnknownState(err) {
			updateErr := r.updateStatus(ctx, &am, appv1alpha1.Suspending)
			if updateErr != nil {
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

	r.runningReconcilesLock.RLock()
	_, exists := r.runningReconciles[req.Name]
	r.runningReconcilesLock.RUnlock()

	defer func() {
		r.runningReconcilesLock.RLock()
		delete(r.runningReconciles, req.Name)
		r.runningReconcilesLock.RUnlock()
	}()

	if exists {
		return nil
	}

	r.runningReconcilesLock.Lock()
	r.runningReconciles[req.Name] = struct{}{}
	r.runningReconcilesLock.Unlock()

	rChan := make(chan error)
	doneChan := make(chan struct{})
	go statefulApp.Exec(pctx, rChan)
	go statefulApp.HandleContext(ctx, rChan, doneChan)

	ret := <-rChan
	klog.Infof("reconciled application manager request name=%s state=%s", req.Name, statefulApp.State())

	return ret
}

type result struct {
	m *appv1alpha1.ApplicationManager
	e error
}

func (r *ApplicationManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	return false
}

func (r *ApplicationManagerController) handleCancel(am *appv1alpha1.ApplicationManager) error {
	klog.Infof("start to perform cancel operation app=%s", am.Spec.AppName)
	ctx := context.Background()
	statefulApp, err := LoadStatefulApp(ctx, r, am.Name)
	if err != nil {
		klog.Errorf("load stateful app failed in handlecancel %v", err)
		return err
	}
	if statefulApp == nil {
		// app not found
		klog.Warningf("reconciling app not found ", am.Name)
		return nil
	}
	rChan := make(chan error)
	statefulApp.Exec(ctx, rChan)
	result := <-rChan
	return result
}

func (r *ApplicationManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	curAppMgr, _ := new.(*appv1alpha1.ApplicationManager)

	if curAppMgr.Spec.Type != appv1alpha1.App {
		return false
	}

	if appstate.IsCanceling(curAppMgr.Status.State) {
		go r.handleCancel(curAppMgr)
		return false
	}

	return true
}

func (r *ApplicationManagerController) rollback(am *appv1alpha1.ApplicationManager) (err error) {
	appConfig := &appcfg.ApplicationConfig{
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
func (r *ApplicationManagerController) updateStatus(ctx context.Context, am *appv1alpha1.ApplicationManager, state appv1alpha1.ApplicationManagerState) error {
	err := r.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		return nil
	}
	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	err = r.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		return err
	}
	return nil
}
