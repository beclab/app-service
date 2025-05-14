package appstate

import (
	"context"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &SuspendingApp{}

type SuspendingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *SuspendingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *SuspendingApp) IsAppCreated() bool {
	//FIXME:
	return true
}

func (p *SuspendingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *SuspendingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *SuspendingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewSuspendingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NewErrorUnknownState(nil, nil)
		}
		klog.Error("get application error: ", err)
		return nil, NewStateError(err.Error())
	}

	return &SuspendingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *SuspendingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	defer func() {
		state := appsv1.Suspended
		message := "app suspend success"
		if err != nil {
			state = appsv1.SuspendFailed
			message = fmt.Sprintf("suspend app %s failed %v", p.manager.Spec.AppName, err)
		}
		updateErr := p.updateStatus(context.TODO(), p.manager, state, nil, message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		c <- err
		return
	}()
	err = suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		c <- fmt.Errorf("suspend app %s failed %w", p.manager.Spec.AppName, err)
	}
	c <- err
	return
}

func (p *SuspendingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *SuspendingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
