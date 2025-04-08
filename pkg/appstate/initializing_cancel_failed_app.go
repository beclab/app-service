package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InitializingCancelFailedApp{}

type InitializingCancelFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InitializingCancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingCancelFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInitializingCancelFailedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {
	return &InitializingCancelFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InitializingCancelFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, appsv1.InitializingCanceling.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.InitializingCanceling, err)
	}
	c <- err
	return
}

func (p *InitializingCancelFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		if err != nil {
			klog.Errorf("app manager %s, do cancel operation failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}

func (p *InitializingCancelFailedApp) Cancel(ctx context.Context) error {
	return nil
}
