package appstate

import (
	"context"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &SuspendingApp{}

type SuspendingApp struct {
	*baseOperationApp
}

func NewSuspendingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {
	// TODO: check app state

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &SuspendingApp{
				&baseOperationApp{
					ttl: ttl,
					baseStatefulApp: &baseStatefulApp{
						manager: manager,
						client:  c,
					},
				},
			}
		})
}

func (p *SuspendingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.exec(ctx)
	if err != nil {
		klog.Errorf("suspend app %s failed %v", p.manager.Spec.AppName, err)
		opRecord := makeRecord(p.manager, appsv1.StopFailed, fmt.Sprintf(constants.OperationFailedTpl, p.manager.Spec.OpType, err.Error()))
		updateErr := p.updateStatus(ctx, p.manager, appsv1.StopFailed, opRecord, err.Error())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.StopFailed, err)
			return nil, updateErr
		}

		return nil, nil
	}

	opRecord := makeRecord(p.manager, appsv1.Stopped, fmt.Sprintf(constants.StopOperationCompletedTpl, p.manager.Spec.AppName))
	updateErr := p.updateStatus(ctx, p.manager, appsv1.Stopped, opRecord, appsv1.Stopped.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopped.String(), err)
		return nil, updateErr
	}

	return nil, nil
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
