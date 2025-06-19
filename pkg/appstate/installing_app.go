package appstate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/errcode"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &InstallingApp{}

type InstallingApp struct {
	*baseOperationApp
}

func NewInstallingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {
	// TODO: check app state

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &InstallingApp{
				baseOperationApp: &baseOperationApp{
					baseStatefulApp: &baseStatefulApp{
						manager: manager,
						client:  c,
					},

					ttl: ttl,
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

						p.finally = func() {
							klog.Infof("app %s pods is still pending, update app state to stopping", p.manager.Spec.AppName)
							updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.Stopping, nil, err.Error())
							if updateErr != nil {
								klog.Errorf("update status failed %v", updateErr)
								return
							}
							utils.PublishAsync(fmt.Sprintf("os.application.%s", p.manager.Spec.AppOwner), p.manager.Spec.AppName, appsv1.Stopping, p.manager.Status)

						}

						return
					}

					p.finally = func() {
						klog.Errorf("app %s install failed, update app state to installFailed", p.manager.Spec.AppName)
						opRecord := makeRecord(p.manager, appsv1.InstallFailed, fmt.Sprintf(constants.OperationFailedTpl, p.manager.Status.OpType, err.Error()))
						updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.InstallFailed, opRecord, err.Error())
						if updateErr != nil {
							klog.Errorf("update status failed %v", updateErr)
							return
						}
						utils.PublishAsync(fmt.Sprintf("os.application.%s", p.manager.Spec.AppOwner), p.manager.Spec.AppName, appsv1.InstallFailed, p.manager.Status)

					}

					return
				} // end of err != nil

				p.finally = func() {
					klog.Infof("app %s install successfully, update app state to initializing", p.manager.Spec.AppName)
					updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.Initializing, nil, appsv1.Initializing.String())
					if updateErr != nil {
						klog.Errorf("update status failed %v", updateErr)
						return
					}
					utils.PublishAsync(fmt.Sprintf("os.application.%s", p.manager.Spec.AppOwner), p.manager.Spec.AppName, appsv1.Initializing, p.manager.Status)

				}
			}()

			return &in, nil
		},
	)
}

func (p *InstallingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InstallingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to installingCanceling state failed %v", err)
		return err
	}
	utils.PublishAsync(fmt.Sprintf("os.application.%s", p.manager.Spec.AppOwner), p.manager.Spec.AppName, appsv1.InitializingCanceling, p.manager.Status)

	return nil
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
