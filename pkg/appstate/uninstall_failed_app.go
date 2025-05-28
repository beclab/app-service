package appstate

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UninstallFailedApp{}

type UninstallFailedApp struct {
	baseStatefulApp
}

func (p *UninstallFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstallFailedApp) IsOperation() bool {
	return true
}

func (p *UninstallFailedApp) IsCancelOperation() bool {
	return false
}

func (p *UninstallFailedApp) IsAppCreated() bool {
	return true
}

func (p *UninstallFailedApp) IsTimeout() bool {
	return false
}

func NewUninstallFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UninstallFailedApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *UninstallFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.StateReconcile(ctx)
	if err != nil {
		klog.Errorf("app manager %s exec state reconcile failed %v", p.manager.Name, err)
	}
	return nil, err
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
