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

func (p *InstallingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *InstallingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewInstallingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &InstallingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *InstallingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	klog.Infof("execute installing cancel operation appName=%s", p.manager.Spec.AppName)

	err := p.handleInstallCancel(ctx)
	state := appsv1.InstallingCanceled

	if err != nil {
		state = appsv1.InstallingCancelFailed
	}

	updateErr := p.updateStatus(ctx, p.manager, state, nil, state.String())

	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, state, err)
		c <- updateErr
		return
	}
	c <- nil
	return
}

func (p *InstallingCancelingApp) handleInstallCancel(ctx context.Context) error {
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	//defer func() {
	//	DelCancelFunc(p.manager.Name)
	//}()
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
			return fmt.Errorf("app %s execute cancel operation failed %w", appCfg.AppName, ctx.Err())
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
		if err != nil {
			klog.Errorf("app manager %s, do cancel operation failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}
