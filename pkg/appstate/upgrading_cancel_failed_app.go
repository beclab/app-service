package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &UpgradingCancelFailedApp{}

type UpgradingCancelFailedApp struct {
	*baseOperationApp
}

func NewUpgradingCancelFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {
	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UpgradingCancelFailedApp{
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

func (p *UpgradingCancelFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.updateStatus(ctx, p.manager, appsv1.UpgradingCanceling, nil, appsv1.UpgradingCanceling.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.UpgradingCanceling, err)
	}
	return nil, err
}
