package appstate

import (
	"context"
	"strings"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/appinstaller/versioned"
	"bytetrade.io/web3os/app-service/pkg/utils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulApp interface {
	GetManager() *appsv1.ApplicationManager
	State() string
	Finally()
}

type baseStatefulApp struct {
	finallyApp
	app     *appsv1.Application
	manager *appsv1.ApplicationManager
	client  client.Client
}

func (b *baseStatefulApp) GetManager() *appsv1.ApplicationManager {
	return b.manager
}

func (b *baseStatefulApp) State() string {
	return b.GetManager().Status.State.String()
}

// func (b *baseStatefulApp) GetApp() *appsv1.Application {
// 	return b.app
// }

func (b *baseStatefulApp) updateStatus(ctx context.Context, am *appsv1.ApplicationManager, state appsv1.ApplicationManagerState,
	opRecord *appsv1.OpRecord, message string) error {
	var err error

	err = b.client.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		return err
	}

	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.Message = message
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	amCopy.Status.OpGeneration += 1
	if opRecord != nil {
		amCopy.Status.OpRecords = append([]appsv1.OpRecord{*opRecord}, amCopy.Status.OpRecords...)
	}
	if len(amCopy.Status.OpRecords) > 20 {
		amCopy.Status.OpRecords = amCopy.Status.OpRecords[:20:20]
	}
	err = b.client.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		klog.Errorf("patch appmgr's  %s status failed %v", am.Name, err)
		return err
	}
	utils.PublishAsync(b.manager.Spec.AppOwner, b.manager.Spec.AppName, string(b.manager.Status.OpType), b.manager.Status.OpID, state.String(), "", nil)

	return nil
}

func (p *baseStatefulApp) forceDeleteApp(ctx context.Context) error {
	token := p.manager.Status.Payload["token"]
	appCfg := &appcfg.ApplicationConfig{
		AppName:   p.manager.Spec.AppName,
		Namespace: p.manager.Spec.AppNamespace,
		OwnerName: p.manager.Spec.AppOwner,
	}

	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	ops, err := versioned.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		return err
	}
	err = ops.Uninstall()
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			klog.Errorf("uninstall app %s failed err %v", appCfg.AppName, err)
			return err
		}
	}
	err = p.updateStatus(ctx, p.manager, appsv1.Uninstalled, nil, appsv1.Uninstalled.String())
	if err != nil {
		klog.Errorf("update app manager %s to state %s failed", p.manager.Name, appsv1.Uninstalled)
		return err
	}
	return nil
}

type OperationApp interface {
	StatefulApp
	IsTimeout() bool
	Exec(ctx context.Context) (StatefulInProgressApp, error)

	// Cancel update the app to cancel state, into the next phase
	Cancel(ctx context.Context) error
}

type baseOperationApp struct {
	*baseStatefulApp
	ttl time.Duration
}

func (b *baseOperationApp) IsTimeout() bool {
	if b.ttl <= 0 {
		return false
	}
	return b.GetManager().Status.StatusTime.Add(b.ttl).Before(time.Now())
}

type CancelOperationApp interface {
	OperationApp
	IsAppCreated() bool
	// Failed() error
}

type StatefulInProgressApp interface {
	OperationApp

	// Cleanup Stop the current operation immediately and clean up the resource if necessary.
	Cleanup(ctx context.Context)
	Done() <-chan struct{}
}

type finallyApp struct {
	finally func()
}

func (f *finallyApp) Finally() {
	if f.finally != nil {
		f.finally()
	}
}

type baseStatefulInProgressApp struct {
	done   func() <-chan struct{}
	cancel context.CancelFunc
}

func (p *baseStatefulInProgressApp) Done() <-chan struct{} {
	if p.done != nil {
		return p.done()
	}

	return nil
}

func (p *baseStatefulInProgressApp) Cleanup(ctx context.Context) {
	if p.cancel != nil {
		p.cancel()
	}
}

// PollableStatefulInProgressApp is an interface for applications that can be polled for their state.
type PollableStatefulInProgressApp interface {
	StatefulInProgressApp
	poll(ctx context.Context) error
	stopPolling()
	WaitAsync(ctx context.Context)
	CreatePollContext() context.Context
}

type basePollableStatefulInProgressApp struct {
	cancelPoll context.CancelFunc
	ctxPoll    context.Context
}

// Cleanup implements PollableStatefulInProgressApp.
func (r *basePollableStatefulInProgressApp) Cleanup(ctx context.Context) {
	r.stopPolling()
}

func (r *basePollableStatefulInProgressApp) stopPolling() {
	if r != nil && r.cancelPoll != nil {
		r.cancelPoll()
	} else {
		klog.Errorf("call cancelPool failed with nil pointer r ")
	}
}

func (p *basePollableStatefulInProgressApp) Done() <-chan struct{} {
	if p.ctxPoll == nil {
		return nil
	}

	return p.ctxPoll.Done()
}

func (p *basePollableStatefulInProgressApp) CreatePollContext() context.Context {
	pollCtx, cancel := context.WithCancel(context.Background())
	p.cancelPoll = cancel
	p.ctxPoll = pollCtx

	return pollCtx
}
