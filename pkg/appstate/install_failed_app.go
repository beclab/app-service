package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InstallFailedApp{}

type InstallFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InstallFailedApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *InstallFailedApp) IsAppCreated() bool {
	return false
}

func (p *InstallFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallFailedApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *InstallFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallFailedApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Error("get application error: ", err)
		return nil, NewStateError(err.Error())
	}

	return &InstallFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *InstallFailedApp) Exec(ctx context.Context, c chan<- error) {
	return
}

func (p *InstallFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InstallFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
