package controllers

import (
	"context"
	"fmt"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/images"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

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

	// start auto reconcile the application manager state
	go wait.Until(r.ReconcileAll, time.Minute, wait.NeverStop)

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
		_, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: appmgr.Name,
			},
		})

		if err != nil {
			klog.Error("reconcile application manager error, ", err, ", ", appmgr.Name)
		}
	} // end of app mgr list loop
}

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("reconcile application manager request name=%s", req.Name)
	statefulApp, err := LoadStatefulApp(ctx, r, req.Name)
	if err != nil {
		klog.Errorf("load stateful app failed %v", err)

		switch {
		case appstate.IsUnknownState(err):
			if srfunc := err.StateReconcile(); srfunc != nil {
				err := srfunc(ctx)
				if err != nil {
					klog.Errorf("reconcile stateful app failed %v", err)
					return ctrl.Result{}, err
				}
			}
		case appstate.IsUnknownInProgressApp(err):
			// this is a special case, the app is in progress but the state is unknown
			err.CleanUp(ctx)
		}

		// return error to the controller-runtime, and re-enqueue the request
		return ctrl.Result{}, err
	}

	if statefulApp == nil {
		// app not found
		klog.Warning("reconciling app not found, ", req.Name)
		return ctrl.Result{}, nil
	}

	if statefulApp.IsOperation() {
		klog.Info("stateful app is doing something, ", statefulApp.State())
		if statefulApp.IsTimeout() {
			if inProgress, ok := statefulApp.(appstate.StatefulInProgressApp); ok {
				klog.Info("stateful app is doing something timeout, should be canceled, ", statefulApp.GetManager().Name, ", ", statefulApp.State())
				err := inProgress.Cancel(ctx)
				if err != nil {
					klog.Info("cancel stateful app operation error, ", err, ", ", statefulApp.GetManager().Name)
				}

				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		inProgress, err := statefulApp.Exec(ctx)
		if err != nil {
			klog.Error("execute stateful app operation error, ", err, ", ", statefulApp.GetManager().Name, ", ", statefulApp.State())
		}

		if inProgress != nil {
			if pollable, ok := inProgress.(appstate.PollableStatefulInProgressApp); ok {
				// use background context to wait for the operation to finish
				// current context `ctx` controlled by the app mgr controller
				pollable.WaitAsync(context.Background())
			}
		}

		return ctrl.Result{}, err
	}

	// FIXME: reconcile the failed state app

	klog.Infof("reconciled application manager request name=%s state=%s", req.Name, statefulApp.State())

	return ctrl.Result{}, nil
}

func (r *ApplicationManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	am, _ := obj.(*appv1alpha1.ApplicationManager)
	return am.Status.State != ""
}

func (r *ApplicationManagerController) handleCancel(am *appv1alpha1.ApplicationManager) (enqueue bool, err error) {
	klog.Infof("start to perform cancel operation app=%s", am.Spec.AppName)
	ctx := context.Background()
	statefulApp, err := LoadStatefulApp(ctx, r, am.Name)
	if err != nil {
		klog.Errorf("load stateful app failed in handle cancel %v", err)
		return false, err
	}

	if statefulApp == nil {
		// app not found
		klog.Warningf("reconciling app %s not found", am.Name)
		return false, nil
	}

	if !statefulApp.IsCancelOperation() {
		return true, nil
	}

	// like installing state, the function `reconcile` will do helm installing synchronously
	// so we need to do cancel operation immediately
	_, serr := statefulApp.Exec(ctx)
	return false, serr
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

	enqueue, err := r.handleCancel(curAppMgr)
	if err != nil {
		klog.Errorf("handle cancel operation failed %v", err)
	}

	return enqueue
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
