package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InstallingCancelFailedApp{}

type InstallingCancelFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InstallingCancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingCancelFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallingCancelFailedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &InstallingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InstallingCancelFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	c <- err
	return
}

func (p *InstallingCancelFailedApp) StateReconcile(ctx context.Context) error {
	err := p.forceDeleteApp(ctx)
	if err != nil {
		klog.Errorf("delete app %s failed %v", p.manager.Spec.AppName, err)
		return err
	}
	return nil
}

func (p *InstallingCancelFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InstallingCancelFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
