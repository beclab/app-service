package appstate

import (
	"context"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UninstallFailedApp{}

type UninstallFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UninstallFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstallFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUninstallFailedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &UninstallFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *UninstallFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	if err != nil {
		klog.Errorf("app manager %s exec state reconcile failed %v", p.manager.Name, err)
	}
	c <- err
	return
}

func (p *UninstallFailedApp) StateReconcile(ctx context.Context) error {
	err := p.forceDeleteApp(ctx)
	if err != nil {
		klog.Errorf("delete app %s failed %v", p.manager.Spec.AppName, err)
		return err
	}
	return err
}

func (p *UninstallFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UninstallFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		if err != nil {
			klog.Errorf("app manager %s cancel failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}
