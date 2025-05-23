package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &PendingCancelingApp{}

type PendingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *PendingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *PendingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewPendingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &PendingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *PendingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel pending not found")
	}
	err := p.updateStatus(ctx, p.manager, appsv1.PendingCanceled, nil, appsv1.PendingCanceled.String())
	if err != nil {
		c <- err
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.PendingCanceled, err)
		return
	}
	c <- nil
	return
}

func (p *PendingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *PendingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
