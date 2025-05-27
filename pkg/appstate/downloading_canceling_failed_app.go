package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DownloadingCancelFailedApp{}

type DownloadingCancelFailedApp struct {
	baseStatefulApp
}

func (p *DownloadingCancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingCancelFailedApp) IsOperation() bool {
	return true
}

func (p *DownloadingCancelFailedApp) IsCancelOperation() bool {
	return false
}

func (p *DownloadingCancelFailedApp) IsAppCreated() bool {
	return false
}

func (p *DownloadingCancelFailedApp) IsTimeout() bool {
	return false
}

func NewDownloadingCancelFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {
	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &DownloadingCancelFailedApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *DownloadingCancelFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceling, nil, appsv1.DownloadingCanceling.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadingCanceling, err)
	}
	return nil, err
}
