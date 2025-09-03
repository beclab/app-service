package controllers

import (
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"fmt"
	"strconv"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AppEnvController struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=appenvs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=appenvs/status,verbs=get;update;patch
//+kubebuilder:groups=app.bytetrade.io,resources=applicationmanagers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=app.bytetrade.io,resources=applicationmanagers/status,verbs=get;update;patch

func (r *AppEnvController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sysv1alpha1.AppEnv{}).
		Complete(r)
}

func (r *AppEnvController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("Reconciling AppEnv: %s", req.NamespacedName)

	var appEnv sysv1alpha1.AppEnv
	if err := r.Get(ctx, req.NamespacedName, &appEnv); err != nil {
		//todo: more detailed logic
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.reconcileAppEnv(ctx, &appEnv)
}

func (r *AppEnvController) reconcileAppEnv(ctx context.Context, appEnv *sysv1alpha1.AppEnv) (ctrl.Result, error) {
	klog.Infof("Processing AppEnv change: %s/%s", appEnv.Namespace, appEnv.Name)

	if err := r.syncSystemEnvValues(ctx, appEnv); err != nil {
		klog.Errorf("Failed to sync SystemEnv values for AppEnv %s/%s: %v", appEnv.Namespace, appEnv.Name, err)
		return ctrl.Result{}, err
	}

	if appEnv.Spec.NeedApply {
		if err := r.triggerApplyEnv(ctx, appEnv); err != nil {
			klog.Errorf("Failed to trigger ApplyEnv for AppEnv %s/%s: %v", appEnv.Namespace, appEnv.Name, err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *AppEnvController) syncSystemEnvValues(ctx context.Context, appEnv *sysv1alpha1.AppEnv) error {
	original := appEnv.DeepCopy()
	var systemEnvList sysv1alpha1.SystemEnvList
	if err := r.List(ctx, &systemEnvList); err != nil {
		return fmt.Errorf("failed to list SystemEnvs: %v", err)
	}

	systemEnvMap := make(map[string]string)
	for _, sysEnv := range systemEnvList.Items {
		value := sysEnv.Spec.Value
		if value == "" {
			value = sysEnv.Spec.Default
		}
		systemEnvMap[sysEnv.Spec.Name] = sysEnv.Spec.Value
	}

	updated := false
	for i := range appEnv.Spec.SystemEnvs {
		sysEnvRef := &appEnv.Spec.SystemEnvs[i]
		if value, exists := systemEnvMap[sysEnvRef.Name]; exists {
			if sysEnvRef.Value != value || sysEnvRef.Status != constants.SystemEnvRefStatusSynced {
				sysEnvRef.Value = value
				sysEnvRef.Status = constants.SystemEnvRefStatusSynced
				updated = true
				if sysEnvRef.ApplyOnChange {
					appEnv.Spec.NeedApply = true
				}
			}
		} else {
			if sysEnvRef.Status != constants.SystemEnvRefStatusNotFound {
				sysEnvRef.Status = constants.SystemEnvRefStatusNotFound
				updated = true
			}
		}
	}

	if updated {
		if err := r.Patch(ctx, appEnv, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to update AppEnv %s/%s: %v", appEnv.Namespace, appEnv.Name, err)
		}
	}

	return nil
}

func (r *AppEnvController) triggerApplyEnv(ctx context.Context, appEnv *sysv1alpha1.AppEnv) error {
	klog.Infof("Triggering ApplyEnv for app: %s owner: %s", appEnv.Spec.AppName, appEnv.Spec.AppOwner)

	appMgrName, err := apputils.FmtAppMgrName(appEnv.Spec.AppName, appEnv.Spec.AppOwner, appEnv.Namespace)
	if err != nil {
		return fmt.Errorf("failed to format app manager name: %v", err)
	}

	var targetAppMgr appv1alpha1.ApplicationManager
	if err := r.Get(ctx, types.NamespacedName{Name: appMgrName}, &targetAppMgr); err != nil {
		return fmt.Errorf("failed to get ApplicationManager %s: %v", appMgrName, err)
	}

	state := targetAppMgr.Status.State
	if !appstate.IsOperationAllowed(state, appv1alpha1.ApplyEnvOp) {
		// trigger backoff retry and this is the expected behaviour
		return fmt.Errorf("app %s is currently in state %s, applyEnv not allowed", appEnv.Spec.AppName, state)
	}

	appMgrCopy := targetAppMgr.DeepCopy()
	appMgrCopy.Spec.OpType = appv1alpha1.ApplyEnvOp

	if err := r.Patch(ctx, appMgrCopy, client.MergeFrom(&targetAppMgr)); err != nil {
		return fmt.Errorf("failed to update ApplicationManager Spec.OpType: %v", err)
	}

	now := metav1.Now()
	opID := strconv.FormatInt(time.Now().Unix(), 10)

	status := appv1alpha1.ApplicationManagerStatus{
		OpType:     appv1alpha1.ApplyEnvOp,
		State:      appv1alpha1.ApplyingEnv,
		OpID:       opID,
		Message:    "waiting for applying env",
		StatusTime: &now,
		UpdateTime: &now,
	}

	// todo: should we consider an apply attempt applied, and set needApply back to false here, or should we keep retrying every time a new reconcile is trigger?
	am, err := apputils.UpdateAppMgrStatus(targetAppMgr.Name, status)
	if err != nil {
		return fmt.Errorf("failed to update ApplicationManager Status: %v", err)
	}
	utils.PublishAppEvent(am.Spec.AppOwner, am.Spec.AppName, string(am.Status.OpType), opID, appv1alpha1.ApplyingEnv.String(), "", nil)

	klog.Infof("Successfully triggered ApplyEnv for app: %s owner: %s", appEnv.Spec.AppName, appEnv.Spec.AppOwner)
	return nil
}
