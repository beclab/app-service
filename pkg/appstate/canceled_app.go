package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &CanceledApp{}

type CanceledApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *CanceledApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *CanceledApp) IsAppCreated() bool {
	return false
}

func (p *CanceledApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *CanceledApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *CanceledApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewCanceledApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &CanceledApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *CanceledApp) Exec(ctx context.Context, c chan<- error) {
	return
}

func (p *CanceledApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}

func (p *CanceledApp) Cancel(ctx context.Context) error {
	return nil
}
