package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &UninstallingApp{}

type UninstallingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UninstallingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstallingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUninstallingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &UninstallingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *UninstallingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	err = p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.UninstallFailed, nil, appsv1.UninstallFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.UninstallFailed.String(), err)
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		c <- err
		return
	}

	updateErr := p.updateStatus(ctx, p.manager, appsv1.Uninstalled, nil, appsv1.Uninstalled.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Uninstalled.String(), err)
		err = errors.Wrapf(err, "update status failed %v", updateErr)
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
		if err != nil {
			klog.Errorf("app manager %s cancel failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}
func (p *UninstallingApp) waitForDeleteNamespace(ctx context.Context) error {
	// cur app may be installed in os-system or user-space-xxx namespace
	// for those app no need to wait namespace be deleted
	if utils.IsProtectedNamespace(p.manager.Spec.AppNamespace) {
		return nil
	}
	err := utilwait.PollImmediate(time.Second, 15*time.Minute, func() (done bool, err error) {
		nsName := p.manager.Spec.AppNamespace
		var ns corev1.Namespace
		err = p.client.Get(ctx, types.NamespacedName{Name: nsName}, &ns)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Error(err)
			return false, err
		}
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil

	})
	return err
}

func (p *UninstallingApp) exec(ctx context.Context) error {
	var err error
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
	if err != nil {
		klog.Errorf("uninstall app %s failed %v", p.manager.Spec.AppName, err)
		return err
	}
	err = p.waitForDeleteNamespace(ctx)
	if err != nil {
		klog.Errorf("waiting app %s namespace %s being deleted failed", p.manager.Spec.AppName, p.manager.Spec.AppNamespace)
		return err
	}
	return nil
}
