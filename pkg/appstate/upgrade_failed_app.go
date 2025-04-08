package appstate

import (
	"context"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UpgradeFailedApp{}

type UpgradeFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UpgradeFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradeFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUpgradeFailedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {
	return &UpgradeFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *UpgradeFailedApp) StateReconcile(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		klog.Errorf("stop upgrade-failed-app %s failed %v", p.manager.Spec.AppNamespace, err)
		return err
	}
	return err
}

func (p *UpgradeFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	if err != nil {
		klog.Errorf("execute upgrade-failed-app %s state reconcile failed %v", p.manager.Spec.AppName, err)
	}
	c <- err
	return
}

func (p *UpgradeFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UpgradeFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
