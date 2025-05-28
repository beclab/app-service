package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UpgradingCancelFailedApp{}

type UpgradingCancelFailedApp struct {
	baseStatefulApp
}

func (p *UpgradingCancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradingCancelFailedApp) IsOperation() bool {
	return true
}

func (p *UpgradingCancelFailedApp) IsCancelOperation() bool {
	return false
}

func (p *UpgradingCancelFailedApp) IsAppCreated() bool {
	return false
}

func (p *UpgradingCancelFailedApp) IsTimeout() bool {
	return false
}

func NewUpgradingCancelFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {
	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UpgradingCancelFailedApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *UpgradingCancelFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.updateStatus(ctx, p.manager, appsv1.UpgradingCanceling, nil, appsv1.UpgradingCanceling.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.UpgradingCanceling, err)
	}
	return nil, err
}
