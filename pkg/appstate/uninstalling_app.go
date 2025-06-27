package appstate

import (
	"context"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &UninstallingApp{}

type UninstallingApp struct {
	*baseOperationApp
}

func NewUninstallingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UninstallingApp{
				&baseOperationApp{
					ttl: ttl,
					baseStatefulApp: &baseStatefulApp{
						manager: manager,
						client:  c,
					},
				},
			}
		})
}

func (p *UninstallingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	opCtx, cancel := context.WithCancel(context.Background())

	return appFactory.execAndWatch(opCtx, p,
		func(c context.Context) (StatefulInProgressApp, error) {
			in := uninstallingInProgressApp{
				UninstallingApp: p,
				baseStatefulInProgressApp: &baseStatefulInProgressApp{
					done:   c.Done,
					cancel: cancel,
				},
			}

			go func() {
				defer cancel()
				err := p.exec(c)
				if err != nil {
					p.finally = func() {
						klog.Infof("uninstalling app %s failed,", p.manager.Spec.AppName)
						opRecord := makeRecord(p.manager, appsv1.UninstallFailed, fmt.Sprintf(constants.OperationFailedTpl, p.manager.Status.OpType, err.Error()))
						updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.UninstallFailed, opRecord, err.Error())
						if updateErr != nil {
							klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.UninstallFailed.String(), err)
							err = errors.Wrapf(err, "update status failed %v", updateErr)
							return
						}

					}
					return
				}

				p.finally = func() {
					klog.Infof("uninstalled app %s success", p.manager.Spec.AppName)
					opRecord := makeRecord(p.manager, appsv1.Uninstalled, fmt.Sprintf(constants.UninstallOperationCompletedTpl, p.manager.Spec.Type, p.manager.Spec.AppName))
					updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.Uninstalled, opRecord, appsv1.Uninstalled.String())
					if updateErr != nil {
						klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Uninstalled.String(), err)
						return
					}

				}
			}()

			return &in, nil
		})
}

func (p *UninstallingApp) waitForDeleteNamespace(ctx context.Context) error {
	// cur app may be installed in os-system or user-space-xxx namespace
	// for those app no need to wait namespace be deleted
	if apputils.IsProtectedNamespace(p.manager.Spec.AppNamespace) {
		return nil
	}
	err := utilwait.PollImmediate(time.Second, 15*time.Minute, func() (done bool, err error) {
		klog.Infof("waiting for namespace %s to be deleted", p.manager.Spec.AppNamespace)
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

func (p *UninstallingApp) Cancel(ctx context.Context) error {
	klog.Infof("cancel uninstalling operation appName=%s", p.manager.Spec.AppName)
	if ok := appFactory.cancelOperation(p.manager.Name); !ok {
		klog.Errorf("app %s operation is not ", p.manager.Name)
	}

	err := p.updateStatus(ctx, p.manager, appsv1.UninstallFailed, nil, appsv1.UninstallFailed.String())
	if err != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.UninstallFailed.String(), err)
	}
	return err
}

var _ StatefulInProgressApp = &uninstallingInProgressApp{}

type uninstallingInProgressApp struct {
	*UninstallingApp
	*baseStatefulInProgressApp
}

// override to avoid duplicate exec
func (p *uninstallingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}
