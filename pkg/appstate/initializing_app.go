package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"context"
	"encoding/json"
	"fmt"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InitializingApp{}

type InitializingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *InitializingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *InitializingApp) IsAppCreated() bool {
	return true
}

func (p *InitializingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *InitializingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInitializingApp(ctx context.Context, client client.Client,
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

	return &InitializingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *InitializingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	defer func() {
		state := appsv1.Running
		message := "app initial success"
		if err != nil {
			state = appsv1.Suspending
			message = fmt.Sprintf("initializing phase failed %v", err)
		}
		klog.Infof("in initializing state....: %v", state)
		updateErr := p.updateStatus(context.TODO(), p.manager, state, nil, message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		c <- err
		return
	}()

	payload := p.manager.Status.Payload
	token := payload["token"]

	var appCfg *appcfg.ApplicationConfig
	err = json.Unmarshal([]byte(p.manager.Spec.Config), &appCfg)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		c <- err
		return
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		c <- err
		return
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{Source: p.manager.Spec.Source})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		c <- err
		return
	}
	ok, err := ops.WaitForLaunch()
	if !ok {
		klog.Errorf("wait for launch failed %v", err)
		c <- err
		return
	}
	c <- nil
	return
}

func (p *InitializingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}

func (p *InitializingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to initializingCanceling state failed %v", err)
		return err
	}
	return nil
}
