package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"fmt"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &UninstallingApp{}

type UninstallingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *UninstallingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *UninstallingApp) IsAppCreated() bool {
	return true
}

func (p *UninstallingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstallingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *UninstallingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUninstallingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &UninstallingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *UninstallingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	defer func() {
		state := appsv1.Uninstalled
		message := "app uninstalled success"
		if err != nil {
			state = appsv1.UninstallFailed
			message = fmt.Sprintf("uninstall app %s failed %v", p.manager.Spec.AppName, err)
		}
		klog.Infof("state in uninstall app exec :%v", state)
		updateErr := p.updateStatus(ctx, p.manager, state, nil, message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		c <- err
		return
	}()
	err = utils.UpdateAppState(ctx, p.manager, appsv1.Uninstalling.String())
	if err != nil {
		c <- err
		return
	}
	token := p.manager.Status.Payload["token"]
	appCfg := &appcfg.ApplicationConfig{
		AppName:   p.manager.Spec.AppName,
		Namespace: p.manager.Spec.AppNamespace,
		OwnerName: p.manager.Spec.AppOwner,
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		c <- err
		return
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		c <- err
		return
	}
	err = ops.Uninstall()
	if err != nil {
		klog.Errorf("uninstall app %s failed %v", p.manager.Spec.AppName, err)
		c <- err
		return
	}
	c <- err
	return
}

func (p *UninstallingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UninstallingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
