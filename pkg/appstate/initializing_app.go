package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InitializingApp{}

type InitializingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InitializingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InitializingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInitializingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &InitializingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InitializingApp) Exec(ctx context.Context, c chan<- error) {
	err := p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, "canceling")
		if err != nil {
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		c <- err
		return
	}

	updateErr := p.updateStatus(ctx, p.manager, appsv1.Running, nil, appsv1.InstallFailed.String())
	if updateErr != nil {
		err = errors.Wrapf(err, "update status failed %v", updateErr)
	}
	c <- err
	return

}

func (p *InitializingApp) exec(ctx context.Context) error {
	var err error
	payload := p.manager.Status.Payload
	token := payload["token"]

	var appCfg *appcfg.ApplicationConfig
	err = json.Unmarshal([]byte(p.manager.Spec.Config), &appCfg)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		return err
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{Source: p.manager.Spec.Source})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		return err
	}
	ok, err := ops.WaitForLaunch()
	if !ok {
		klog.Errorf("wait for launch failed %v", err)
		return err
	}

	return nil
}

func (p *InitializingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		if err != nil {
			klog.Errorf("app manager %s, do cancel operation failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}

func (p *InitializingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.InitializingCanceling, err)
		return err
	}
	return nil
}
