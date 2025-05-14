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

var _ StatefulApp = &InitializingCancelingApp{}

type InitializingCancelingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *InitializingCancelingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *InitializingCancelingApp) IsAppCreated() bool {
	return false
}

func (p *InitializingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingCancelingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *InitializingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInitializingCancelingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Error("获取应用错误: ", err)
		return nil, NewStateError(err.Error())
	}

	return &InitializingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *InitializingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	klog.Infof("execute initializing operation appName=%s", p.manager.Spec.AppName)

	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel initializing not found")
	}

	err := p.updateStatus(ctx, p.manager, appsv1.Suspending, nil, appsv1.Suspending.String())
	if err != nil {
		klog.Errorf("update appmgr state to suspending state failed %v", err)
		c <- err
		return
	}
	c <- nil
	return
}

func (p *InitializingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InitializingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
