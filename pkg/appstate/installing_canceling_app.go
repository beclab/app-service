package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &InstallingCancelingApp{}

type InstallingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *InstallingCancelingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *InstallingCancelingApp) IsAppCreated() bool {
	return false
}

func (p *InstallingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingCancelingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *InstallingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallingCancelingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &InstallingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *InstallingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	klog.Infof("execute installing cancel operation appName=%s", p.manager.Spec.AppName)

	err := p.handleInstallCancel(ctx)
	var updateErr error
	if err != nil {
		updateErr = p.updateStatus(ctx, p.manager, appsv1.InstallingCancelFailed, nil, appsv1.InstallingCancelFailed.String())
	} else {
		updateErr = p.updateStatus(ctx, p.manager, appsv1.InstallingCanceled, nil, appsv1.InstallingCanceled.String())
	}
	if updateErr != nil {
		c <- updateErr
		return
	}
	c <- nil
	return
}

func (p *InstallingCancelingApp) handleInstallCancel(ctx context.Context) error {
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel installing not found")
	}
	pctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	token := p.manager.Status.Payload["token"]
	appCfg := &appcfg.ApplicationConfig{
		AppName:   p.manager.Spec.AppName,
		Namespace: p.manager.Spec.AppNamespace,
		OwnerName: p.manager.Spec.AppOwner,
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		return err
	}
	err = ops.Uninstall()
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Errorf("execute uninstall failed %v", err)
		return err
	}
	if utils.IsProtectedNamespace(p.manager.Spec.AppNamespace) {
		return nil
	}

	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			var ns corev1.Namespace
			err = p.client.Get(pctx, types.NamespacedName{Name: p.manager.Spec.AppNamespace}, &ns)
			if apierrors.IsNotFound(err) {
				return nil
			}
		case <-pctx.Done():
			return fmt.Errorf("execute cancel operation failed %w", ctx.Err())
		}
	}
}

func (p *InstallingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *InstallingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
