package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UpgradingCancelingApp{}

type UpgradingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UpgradingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUpgradingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &UpgradingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *UpgradingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	klog.Infof("execute upgrading cancel operation appName=%s", p.manager.Spec.AppName)

	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel upgrading not found")
	}

	err = p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
	if err != nil {
		klog.Errorf("update appmgr state to suspending state failed %v", err)
		c <- err
		return
	}
	c <- err
	return
}

func (p *UpgradingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UpgradingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
