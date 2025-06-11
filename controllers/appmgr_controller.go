package controllers

import (
	"context"
	"fmt"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/images"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

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
	go wait.Until(r.ReconcileAll, 2*time.Minute, wait.NeverStop)

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
			}})
		if err != nil {
			klog.Error("reconcile application manager error, ", err, ", ", appmgr.Name)
		}
		time.Sleep(time.Second)
	} // end of app mgr list loop
}

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("reconcile application manager request name=%s", req.Name)
	statefulApp, err := r.loadStatefulAppAndReconcile(ctx, req.Name)
	if err != nil {
		klog.Errorf("load stateful app failed in reconcile %v", err)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}

	if operation, ok := statefulApp.(appstate.OperationApp); ok {
		klog.Info("stateful app is doing something, ", statefulApp.State())
		if operation.IsTimeout() {
			klog.Errorf("stateful app is timeout: %v, state:%v", req.Name, statefulApp.State())
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

		inProgress, err := operation.Exec(ctx)
		if err != nil {
			klog.Error("execute stateful app operation error, ", err, ", ", statefulApp.GetManager().Name, ", ", statefulApp.State())

			if waiting, ok := err.(appstate.RequeueError); ok {
				// if the error is a requeue error, we should requeue the request
				return ctrl.Result{RequeueAfter: waiting.RequeueAfter()}, nil
			}
		}

		if inProgress != nil {
			if pollable, ok := inProgress.(appstate.PollableStatefulInProgressApp); ok {
				// use background context to wait for the operation to finish
				// current context `ctx` controlled by the app mgr controller
				c := pollable.CreatePollContext()
				pollable.WaitAsync(c)
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

func (r *ApplicationManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	oldAppMgr, _ := old.(*appv1alpha1.ApplicationManager)
	curAppMgr, _ := new.(*appv1alpha1.ApplicationManager)

	if curAppMgr.Spec.Type != appv1alpha1.App {
		return false
	}
	if curAppMgr.Status.OpGeneration <= oldAppMgr.Status.OpGeneration {
		return false
	}

	return true
}

func (r *ApplicationManagerController) loadStatefulAppAndReconcile(ctx context.Context, name string) (appstate.StatefulApp, error) {
	statefulApp, err := LoadStatefulApp(ctx, r, name)
	if err != nil {
		klog.Errorf("load stateful app failed %v", err)

		switch {
		case appstate.IsUnknownState(err):
			if srfunc := err.StateReconcile(); srfunc != nil {
				err := srfunc(ctx)
				if err != nil {
					klog.Errorf("reconcile stateful app failed %v", err)
					return nil, err
				}
			}
		case appstate.IsUnknownInProgressApp(err):
			// this is a special case, the app is in progress but the state is unknown
			err.CleanUp(ctx)
		}

		// return error to the controller-runtime, and re-enqueue the request
		return nil, err
	}

	return statefulApp, nil
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
