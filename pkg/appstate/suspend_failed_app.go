package appstate

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &SuspendFailedApp{}

type SuspendFailedApp struct {
	baseStatefulApp
}

func (p *SuspendFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *SuspendFailedApp) IsOperation() bool {
	return true
}

func (p *SuspendFailedApp) IsCancelOperation() bool {
	return true
}

func (p *SuspendFailedApp) IsAppCreated() bool {
	return true
}

func (p *SuspendFailedApp) IsTimeout() bool {
	return false
}

func NewSuspendFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &SuspendFailedApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *SuspendFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.StateReconcile(ctx)
	if err != nil {
		klog.Errorf("stop-failed-app %s state reconcile failed %v", p.manager.Spec.AppName, err)
	}
	return nil, err
}

func (p *SuspendFailedApp) StateReconcile(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		klog.Errorf("stop-failed-app %s state reconcile failed %v", p.manager.Spec.AppName, err)
	}
	return err
}

func (p *SuspendFailedApp) Cancel(ctx context.Context) error {
	return nil
}
