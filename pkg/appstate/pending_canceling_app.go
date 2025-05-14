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

var _ StatefulApp = &PendingCancelingApp{}

type PendingCancelingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *PendingCancelingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *PendingCancelingApp) IsAppCreated() bool {
	return false
}

func (p *PendingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *PendingCancelingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *PendingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewPendingCancelingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &PendingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
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
		c <- err
	case <-done:
		return
	}
}
