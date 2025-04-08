package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	//"k8s.io/klog/v2"
)

var _ StatefulApp = &ResumingCancelingApp{}

type ResumingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *ResumingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *ResumingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewResumingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &ResumingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *ResumingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel resuming not found")
	}

	err = p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopping.String(), err)
		c <- err
		return
	}
	c <- err
	return
}

func (p *ResumingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *ResumingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
