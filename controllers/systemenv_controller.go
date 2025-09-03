package controllers

import (
	"context"
	"fmt"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SystemEnvController struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=systemenvs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=systemenvs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=appenvs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=appenvs/status,verbs=get;update;patch

func (r *SystemEnvController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sysv1alpha1.SystemEnv{}).
		Complete(r)
}

func (r *SystemEnvController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("Reconciling SystemEnv: %s", req.NamespacedName)

	var systemEnv sysv1alpha1.SystemEnv
	if err := r.Get(ctx, req.NamespacedName, &systemEnv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.reconcileSystemEnv(ctx, &systemEnv)
}

func (r *SystemEnvController) reconcileSystemEnv(ctx context.Context, systemEnv *sysv1alpha1.SystemEnv) (ctrl.Result, error) {
	klog.Infof("Processing SystemEnv change: %s", systemEnv.Spec.Name)

	var appEnvList sysv1alpha1.AppEnvList
	if err := r.List(ctx, &appEnvList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list AppEnvs: %v", err)
	}

	refCount := 0
	updatedCount := 0
	failedCount := 0

	for i := range appEnvList.Items {
		appEnv := &appEnvList.Items[i]
		if r.isReferenced(appEnv, systemEnv.Spec.Name) {
			refCount++

			updated, err := r.updateRefValueInAppEnvIfNeeded(ctx, appEnv, systemEnv)
			if updated {
				updatedCount++
			}
			if err != nil {
				klog.Errorf("Failed to check if AppEnv %s/%s needs update: %v", appEnv.Namespace, appEnv.Name, err)
				failedCount++
				continue
			}
		}
	}

	if refCount > 0 {
		klog.Infof("SystemEnv %s reconciliation completed: %d total references, %d updatedCount, %d failed",
			systemEnv.Spec.Name, refCount, updatedCount, failedCount)
	}

	if failedCount > 0 {
		return ctrl.Result{}, fmt.Errorf("failed to update %d AppEnvs referencing SystemEnv %s", failedCount, systemEnv.Spec.Name)
	}

	return ctrl.Result{}, nil
}

func (r *SystemEnvController) isReferenced(appEnv *sysv1alpha1.AppEnv, systemEnvName string) bool {
	for _, sysEnvRef := range appEnv.Spec.SystemEnvs {
		if sysEnvRef.Name == systemEnvName {
			return true
		}
	}
	return false
}

func (r *SystemEnvController) updateRefValueInAppEnvIfNeeded(ctx context.Context, appEnv *sysv1alpha1.AppEnv, systemEnv *sysv1alpha1.SystemEnv) (bool, error) {
	for i := range appEnv.Spec.SystemEnvs {
		if appEnv.Spec.SystemEnvs[i].Name == systemEnv.Spec.Name {
			currentValue := appEnv.Spec.SystemEnvs[i].Value
			newValue := systemEnv.Spec.Value
			if newValue == "" {
				newValue = systemEnv.Spec.Default
			}

			if currentValue != newValue {
				original := appEnv.DeepCopy()
				klog.Infof("AppEnv %s/%s SystemEnv %s value differs: current='%s', new='%s'",
					appEnv.Namespace, appEnv.Name, systemEnv.Spec.Name, currentValue, newValue)
				appEnv.Spec.SystemEnvs[i].Value = newValue
				if appEnv.Spec.SystemEnvs[i].ApplyOnChange {
					appEnv.Spec.NeedApply = true
				}
				return true, r.Patch(ctx, appEnv, client.MergeFrom(original))
			}

			klog.V(4).Infof("AppEnv %s/%s SystemEnv %s value is already up-to-date: '%s'",
				appEnv.Namespace, appEnv.Name, systemEnv.Spec.Name, currentValue)
			return false, nil
		}
	}

	// this should never be reached
	klog.Warningf("SystemEnv %s not found in AppEnv %s/%s")
	return false, nil
}
