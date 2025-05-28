package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UpgradingCancelingApp{}

type UpgradingCancelingApp struct {
	baseStatefulApp
}

func (p *UpgradingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradingCancelingApp) IsOperation() bool {
	return true
}

func (p *UpgradingCancelingApp) IsCancelOperation() bool {
	return true
}

func (p *UpgradingCancelingApp) IsAppCreated() bool {
	return true
}

func (p *UpgradingCancelingApp) IsTimeout() bool {
	return false
}

func NewUpgradingCancelingApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UpgradingCancelingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *UpgradingCancelingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	var err error
	klog.Infof("execute upgrading cancel operation appName=%s", p.manager.Spec.AppName)

	if ok := appFactory.cancelOperation(p.manager.Name); !ok {
		klog.Errorf("app %s operation is not ", p.manager.Name)
	}

	err = p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
	if err != nil {
		klog.Errorf("update appmgr state to suspending state failed %v", err)
		return nil, err
	}

	return nil, nil
}

func (p *UpgradingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}
