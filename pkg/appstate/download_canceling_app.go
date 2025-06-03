package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ CancelOperationApp = &DownloadingCancelingApp{}

type DownloadingCancelingApp struct {
	*baseOperationApp
}

func (p *DownloadingCancelingApp) IsAppCreated() bool {
	return false
}

func NewDownloadingCancelingApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &DownloadingCancelingApp{
				baseOperationApp: &baseOperationApp{
					baseStatefulApp: &baseStatefulApp{
						manager: manager,
						client:  c,
					},
					ttl: ttl,
				},
			}
		})
}

func (p *DownloadingCancelingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	if ok := appFactory.cancelOperation(p.manager.Name); !ok {
		klog.Errorf("app %s operation is not ", p.manager.Name)
	}
	updateErr := p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceled, nil, appsv1.DownloadingCanceled.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadingCanceled.String(), updateErr)

		return nil, updateErr
	}

	return nil, nil
}

// func (p *DownloadingCancelingApp) Cancel(ctx context.Context) error {
// 	return p.updateStatus(ctx, p.manager, appsv1.DownloadingCancelFailed, nil, appsv1.DownloadingCancelFailed.String())
// }
