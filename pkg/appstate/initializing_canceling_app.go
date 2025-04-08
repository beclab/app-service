package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InitializingCancelingApp{}

type InitializingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InitializingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInitializingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &InitializingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InitializingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	klog.Infof("execute initializing operation appName=%s", p.manager.Spec.AppName)

	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	//defer func() {
	//	DelCancelFunc(p.manager.Name)
	//}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel initializing not found")
	}

	err := p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopping.String(), err)
		c <- err
		return
	}
	c <- nil
	return
}

func (p *InitializingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InitializingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
