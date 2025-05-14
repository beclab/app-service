package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &RunningApp{}

type RunningApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *RunningApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *RunningApp) IsAppCreated() bool {
	return true
}

func (p *RunningApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *RunningApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *RunningApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewRunningApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NewErrorUnknownState(nil, nil)
		}
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &RunningApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *RunningApp) Exec(ctx context.Context, c chan<- error) {
	c <- nil
	return
}

func (p *RunningApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *RunningApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
