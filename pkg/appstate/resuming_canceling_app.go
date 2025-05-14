package appstate

import (
	"context"
	"k8s.io/klog/v2"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	//"k8s.io/klog/v2"
)

var _ StatefulApp = &ResumingCancelingApp{}

type ResumingCancelingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *ResumingCancelingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *ResumingCancelingApp) IsAppCreated() bool {
	return false
}

func (p *ResumingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *ResumingCancelingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *ResumingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewResumingCancelingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &ResumingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
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

	err = p.updateStatus(ctx, p.manager, appsv1.Suspending, nil, appsv1.Suspending.String())
	if err != nil {
		klog.Errorf("update appmgr state to suspending state failed %v", err)
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
		c <- err
	case <-done:
		return
	}
}
