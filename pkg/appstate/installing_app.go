package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/errcode"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InstallingApp{}

type InstallingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InstallingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &InstallingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InstallingApp) Exec(ctx context.Context, c chan<- error) {
	err := p.exec(ctx)
	if err != nil {
		if errors.Is(err, errcode.ErrPodPending) {
			updateErr := p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, err.Error())
			if updateErr != nil {
				err = errors.Wrapf(err, "update status failed %v", updateErr)
			}
			c <- err
			return
		}
		updateErr := p.updateStatus(ctx, p.manager, appsv1.InstallFailed, nil, appsv1.InstallFailed.String())
		if updateErr != nil {
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		c <- err
		return
	}
	updateErr := p.updateStatus(ctx, p.manager, appsv1.Initializing, nil, appsv1.Initializing.String())
	if updateErr != nil {
		err = errors.Wrapf(err, "update status failed %v", updateErr)
	}
	c <- err
	return

}

func (p *InstallingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InstallingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to installingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *InstallingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
func (p *InstallingApp) exec(ctx context.Context) error {
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

	err = setExposePorts(ctx, appCfg)
	if err != nil {
		klog.Errorf("set expose ports failed %v", err)
		return err
	}
	err = ops.Install()
	if err != nil {
		klog.Errorf("install app %s failed %v", p.manager.Spec.AppName, err)
		return err
	}
	return nil
}
