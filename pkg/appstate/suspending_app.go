package appstate

import (
	"context"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &SuspendingApp{}

type SuspendingApp struct {
	baseStatefulApp
}

func (p *SuspendingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *SuspendingApp) IsOperation() bool {
	return true
}

func (p *SuspendingApp) IsCancelOperation() bool {
	return false
}

func (p *SuspendingApp) IsAppCreated() bool {
	return true
}

func (p *SuspendingApp) IsTimeout() bool {
	return false
}

func NewSuspendingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {
	// TODO: check app state

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &SuspendingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *SuspendingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.exec(ctx)
	if err != nil {
		klog.Errorf("suspend app %s failed %v", p.manager.Spec.AppName, err)
		updateErr := p.updateStatus(ctx, p.manager, appsv1.StopFailed, nil, appsv1.StopFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.StopFailed, err)
		}
		return nil, updateErr
	}
	//message := fmt.Sprintf(constants.SuspendOperationCompletedTpl, p.manager.Spec.AppName)
	//opRecord := MakeRecord(appsv1.StopOp, p.manager.Spec.Source, p.manager.Status.Payload["version"], appsv1.Stopped, message)
	updateErr := p.updateStatus(ctx, p.manager, appsv1.Stopped, nil, appsv1.Stopped.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopped.String(), err)
	}
	return nil, updateErr
}

func (p *SuspendingApp) exec(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		klog.Errorf("suspend app %s failed %v", p.manager.Spec.AppName, err)
		return fmt.Errorf("suspend app %s failed %w", p.manager.Spec.AppName, err)
	}
	return nil
}

func (p *SuspendingApp) Cancel(ctx context.Context) error {
	// FIXME: cancel suspend operation if timeout
	return nil
}
