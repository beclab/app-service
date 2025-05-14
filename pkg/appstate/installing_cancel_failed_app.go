package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InstallingCancelFailedApp{}

type InstallingCancelFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InstallingCancelFailedApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *InstallingCancelFailedApp) IsAppCreated() bool {
	return false
}

func (p *InstallingCancelFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingCancelFailedApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *InstallingCancelFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallingCancelFailedApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &InstallingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *InstallingCancelFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.forceDeleteApp(ctx)
	if err != nil {
		c <- err
		return
	}
	err = p.updateStatus(ctx, p.manager, appsv1.Uninstalled, nil, appsv1.Uninstalled.String())
	c <- err
	return
}

func (p *InstallingCancelFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InstallingCancelFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
