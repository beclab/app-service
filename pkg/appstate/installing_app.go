package appstate

import (
	"time"

	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/errcode"

	"context"
	"encoding/json"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InstallingApp{}

type InstallingApp struct {
	baseStatefulApp
	ttl time.Duration
}

func (p *InstallingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingApp) IsOperation() bool {
	return true
}

func (p *InstallingApp) IsCancelOperation() bool {
	return false
}

func (p *InstallingApp) IsAppCreated() bool {
	return false
}

func (p *InstallingApp) IsTimeout() bool {
	if p.ttl == 0 {
		return false
	}
	return p.manager.Status.StatusTime.Add(p.ttl).Before(time.Now())
}

func NewInstallingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {
	// TODO: check app state

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &InstallingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *InstallingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	var err error
	payload := p.manager.Status.Payload
	token := payload["token"]
	var appCfg *appcfg.ApplicationConfig
	err = json.Unmarshal([]byte(p.manager.Spec.Config), &appCfg)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		return nil, err
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return nil, err
	}
	err = setExposePorts(ctx, appCfg)
	if err != nil {
		klog.Errorf("set expose ports failed %v", err)
		return nil, err
	}

	opCtx, cancel := context.WithCancel(context.Background())

	ops, err := appinstaller.NewHelmOps(opCtx, kubeConfig, appCfg, token, appinstaller.Opt{Source: p.manager.Spec.Source})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		cancel()
		return nil, err
	}

	return appFactory.execAndWatch(opCtx, p,
		func(c context.Context) (StatefulInProgressApp, error) {
			in := installingInProgressApp{
				InstallingApp: p,
				baseStatefulInProgressApp: &baseStatefulInProgressApp{
					done:   c.Done,
					cancel: cancel,
				},
			}

			go func() {
				defer cancel()
				err = ops.Install()
				if err != nil {
					klog.Errorf("install app %s failed %v", p.manager.Spec.AppName, err)
					if errors.Is(err, errcode.ErrPodPending) {
						updateErr := p.updateStatus(c, p.manager, appsv1.Stopping, nil, err.Error())
						if updateErr != nil {
							klog.Errorf("update status failed %v", updateErr)
						}

						return
					}

					updateErr := p.updateStatus(c, p.manager, appsv1.InstallFailed, nil, appsv1.InstallFailed.String())
					if updateErr != nil {
						klog.Errorf("update status failed %v", updateErr)
					}

					return
				} // end of err != nil

				updateErr := p.updateStatus(c, p.manager, appsv1.Initializing, nil, appsv1.Initializing.String())
				if updateErr != nil {
					klog.Errorf("update status failed %v", updateErr)
				}
			}()

			return &in, nil
		},
		nil,
	)
}

var _ StatefulInProgressApp = &installingInProgressApp{}

type installingInProgressApp struct {
	*InstallingApp
	*baseStatefulInProgressApp
}

// override to avoid duplicate exec
func (p *installingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}

func (p *installingInProgressApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InstallingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to installingCanceling state failed %v", err)
		return err
	}
	return nil
}
