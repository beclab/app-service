package appstate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/appinstaller/versioned"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/utils/config"

	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &UpgradingApp{}

type UpgradingApp struct {
	*baseOperationApp
	imageClient images.ImageManager
}

func (p *UpgradingApp) State() string {
	return p.GetManager().Status.State.String()
}

func NewUpgradingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UpgradingApp{
				baseOperationApp: &baseOperationApp{
					ttl: ttl,
					baseStatefulApp: &baseStatefulApp{
						manager: manager,
						client:  c,
					},
				},
				imageClient: images.NewImageManager(c),
			}
		})
}

func (p *UpgradingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {

	opCtx, cancel := context.WithCancel(context.Background())
	return appFactory.execAndWatch(opCtx, p,
		func(c context.Context) (StatefulInProgressApp, error) {
			in := upgradingInProgressApp{
				UpgradingApp: p,
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
						klog.Info("upgrade app failed, update app status to upgradeFailed, ", p.manager.Name)
						opRecord := makeRecord(p.manager, appsv1.UpgradeFailed, fmt.Sprintf(constants.OperationFailedTpl, p.manager.Status.OpType, err.Error()))

						updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.UpgradeFailed, opRecord, err.Error())
						if updateErr != nil {
							klog.Errorf("update appmgr state to upgradeFailed state failed %v", updateErr)
							return
						}

					}
					return
				}

				p.finally = func() {
					klog.Info("upgrade app success, update app status to initializing, ", p.manager.Name)
					updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.Initializing, nil, appsv1.Initializing.String())
					if updateErr != nil {
						klog.Errorf("update appmgr state to initializing state failed %v", updateErr)
						return
					}

				}

			}()

			return &in, nil
		})
}

func (p *UpgradingApp) exec(ctx context.Context) error {
	var err error
	var version string
	var actionConfig *action.Configuration
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	actionConfig, _, err = helm.InitConfig(kubeConfig, p.manager.Spec.AppNamespace)
	if err != nil {
		klog.Errorf("helm init config failed %v", err)
		return err
	}
	var appConfig *appcfg.ApplicationConfig
	deployedVersion, _, err := apputils.GetDeployedReleaseVersion(actionConfig, p.manager.Spec.AppName)
	if err != nil {
		klog.Errorf("Failed to get release revision err=%v", err)
		return err
	}

	if !utils.MatchVersion(version, ">= "+deployedVersion) {
		err = errors.New("upgrade version should great than deployed version")
		return err
	}

	payload := p.manager.Status.Payload
	version = payload["version"]
	cfgURL := payload["cfgURL"]
	repoURL := payload["repoURL"]
	token := payload["token"]
	marketSource := payload["marketSource"]
	var chartPath string
	admin, err := kubesphere.GetAdminUsername(ctx, kubeConfig)
	if err != nil {
		klog.Errorf("get admin username failed %v", err)
		return err
	}
	isAdmin, err := kubesphere.IsAdmin(ctx, kubeConfig, p.manager.Spec.AppOwner)
	if err != nil {
		klog.Errorf("failed check is admin user %v", err)
		return err
	}
	if !userspace.IsSysApp(p.manager.Spec.AppName) {
		appConfig, chartPath, err = config.GetAppConfig(ctx, p.manager.Spec.AppName, p.manager.Spec.AppOwner, cfgURL, repoURL, version, token, admin, marketSource, isAdmin)
		if err != nil {
			klog.Errorf("get app config failed %v", err)
			return err
		}
	} else {
		chartPath, err = apputils.GetIndexAndDownloadChart(ctx, p.manager.Spec.AppName, repoURL, version, token, p.manager.Spec.AppOwner, marketSource)
		if err != nil {
			klog.Errorf("download chart failed %v", err)
			return err
		}
		err = json.Unmarshal([]byte(p.manager.Spec.Config), &appConfig)
		if err != nil {
			klog.Errorf("unmarshal to appConfig failed %v", err)
			return err
		}
	}
	ops, err := versioned.NewHelmOps(ctx, kubeConfig, appConfig, token, appinstaller.Opt{Source: p.manager.Spec.Source})
	if err != nil {
		klog.Errorf("make helmop failed %v", err)
		return err
	}
	values := map[string]interface{}{
		"admin": admin,
		"bfl": map[string]string{
			"username": p.manager.Spec.AppOwner,
		},
	}
	refs, err := utils.GetRefFromResourceList(chartPath, values)
	if err != nil {
		klog.Errorf("get image refs from resources failed %v", err)
		return err
	}
	err = p.imageClient.Create(ctx, p.manager, refs)
	if err != nil {
		klog.Errorf("create imagemanager failed %v", err)
		return err
	}
	err = p.imageClient.PollDownloadProgress(ctx, p.manager)
	if err != nil {
		klog.Errorf("poll image download progress failed %v", err)
		return err
	}
	err = ops.Upgrade()
	if err != nil {
		klog.Errorf("upgrade app %s failed %v", p.manager.Spec.AppName, err)
		return err
	}
	return nil
}

func (p *UpgradingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.UpgradingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to upgradingCanceling state failed %v", err)
		return err
	}
	return nil
}

var _ StatefulInProgressApp = &upgradingInProgressApp{}

type upgradingInProgressApp struct {
	*UpgradingApp
	*baseStatefulInProgressApp
}

// override to avoid duplicate exec
func (p *upgradingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}
