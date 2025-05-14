package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UninstalledApp{}

type UninstalledApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UninstalledApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *UninstalledApp) IsAppCreated() bool {
	return false
}

func (p *UninstalledApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstalledApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *UninstalledApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUninstalledApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return &UninstalledApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}, nil
}

func (p *UninstalledApp) Exec(ctx context.Context, c chan<- error) {
	c <- nil
	return
}

func (p *UninstalledApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UninstalledApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
