package appstate

import (
	"context"
	"encoding/json"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &InitializingApp{}

type InitializingApp struct {
	baseStatefulApp
	ttl time.Duration
}

// IsAppCreated implements StatefulApp.
func (p *InitializingApp) IsAppCreated() bool {
	return true
}

// IsCancelOperation implements StatefulApp.
func (p *InitializingApp) IsCancelOperation() bool {
	return false
}

// IsOperation implements StatefulApp.
func (p *InitializingApp) IsOperation() bool {
	return true
}

// IsTimeout implements StatefulApp.
func (p *InitializingApp) IsTimeout() bool {
	if p.ttl == 0 {
		return false
	}
	return p.manager.Status.StatusTime.Add(p.ttl).Before(time.Now())
}

func (p *InitializingApp) State() string {
	return p.GetManager().Status.State.String()
}

func NewInitializingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, error) {
	// TODO: check app state

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &InitializingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *InitializingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
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

	opCtx, cancel := context.WithCancel(context.Background())

	ops, err := appinstaller.NewHelmOps(opCtx, kubeConfig, appCfg, token, appinstaller.Opt{Source: p.manager.Spec.Source})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		cancel()
		return nil, err
	}

	return appFactory.execAndWatch(opCtx, p,
		func(c context.Context) (StatefulInProgressApp, error) {
			in := initializingInProgressApp{
				InitializingApp: p,
				done:            c.Done,
				cancel:          cancel,
			}

			go func() {
				defer cancel()

				ok, err := ops.WaitForLaunch()
				if !ok {
					klog.Errorf("wait for launch failed %v", err)
					if err != nil {
						klog.Error("wait for launch error: ", err, ", ", p.manager.Name)
						updateErr := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, "canceling")
						if updateErr != nil {
							klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.InitializingCanceling, updateErr)
							return
						}
					}

					return

				}
				updateErr := p.updateStatus(ctx, p.manager, appsv1.Running, nil, appsv1.InstallFailed.String())
				if updateErr != nil {
					klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Running, updateErr)
				}
			}()

			return &in, nil
		},
		nil,
	)

}

var _ StatefulInProgressApp = &initializingInProgressApp{}

type initializingInProgressApp struct {
	*InitializingApp
	done   func() <-chan struct{}
	cancel context.CancelFunc
}

// override to avoid duplicate exec
func (p *initializingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}

func (p *initializingInProgressApp) Done() <-chan struct{} {
	if p.done != nil {
		return p.done()
	}

	return nil
}

func (p *initializingInProgressApp) Cleanup(ctx context.Context) {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *initializingInProgressApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.InitializingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.InitializingCanceling, err)
		return err
	}
	return nil
}
