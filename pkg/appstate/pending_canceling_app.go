package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &PendingCancelingApp{}

type PendingCancelingApp struct {
	baseStatefulApp
}

func (p *PendingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *PendingCancelingApp) IsOperation() bool {
	return true
}

func (p *PendingCancelingApp) IsCancelOperation() bool {
	return true
}

func (p *PendingCancelingApp) IsAppCreated() bool {
	return true
}

func (p *PendingCancelingApp) IsTimeout() bool {
	return false
}

func NewPendingCancelingApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &PendingCancelingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *PendingCancelingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	if ok := appFactory.cancelOperation(p.manager.Name); !ok {
		klog.Errorf("app %s operation is not ", p.manager.Name)
	}

	err := p.updateStatus(ctx, p.manager, appsv1.PendingCanceled, nil, appsv1.PendingCanceled.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.PendingCanceled, err)
		return nil, err
	}

	return nil, nil
}

func (p *PendingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}
