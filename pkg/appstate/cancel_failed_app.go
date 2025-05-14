package appstate

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &CancelFailedApp{}

type CancelFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *CancelFailedApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *CancelFailedApp) IsAppCreated() bool {
	return false
}

func (p *CancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *CancelFailedApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *CancelFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewCancelFailedApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &CancelFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}, nil
}

func (p *CancelFailedApp) Exec(ctx context.Context, c chan<- error) {
	return
}

func (p *CancelFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}

func (p *CancelFailedApp) Cancel(ctx context.Context) error {
	return nil
}
