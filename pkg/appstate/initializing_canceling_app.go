package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InitializingCancelingApp{}

type InitializingCancelingApp struct {
	baseStatefulApp
}

func (p *InitializingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingCancelingApp) IsOperation() bool {
	return true
}

func (p *InitializingCancelingApp) IsCancelOperation() bool {
	return true
}

func (p *InitializingCancelingApp) IsAppCreated() bool {
	return true
}

func (p *InitializingCancelingApp) IsTimeout() bool {
	return false
}

func NewInitializingCancelingApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &InitializingCancelingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *InitializingCancelingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	klog.Infof("execute initializing operation appName=%s", p.manager.Spec.AppName)

	if ok := appFactory.cancelOperation(p.manager.Name); !ok {
		klog.Errorf("app %s operation is not ", p.manager.Name)
	}

	err := p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopping.String(), err)
		return nil, err
	}

	return nil, nil
}

func (p *InitializingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}
