package appstate

import (
	"context"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &CanceledApp{}

type CanceledApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *CanceledApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *CanceledApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewCanceledApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &CanceledApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *CanceledApp) Exec(ctx context.Context, c chan<- error) {
	c <- nil
	return
}

func (p *CanceledApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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

func (p *CanceledApp) Cancel(ctx context.Context) error {
	return nil
}
