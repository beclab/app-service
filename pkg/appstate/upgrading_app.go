package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &UpgradingApp{}

type UpgradingApp struct {
	StatefulApp
	baseStatefulApp
	ttl         time.Duration
	imageClient images.ImageManager
}

func (p *UpgradingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUpgradingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &UpgradingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
		imageClient: images.NewImageManager(client),
	}
}

func (p *UpgradingApp) Exec(ctx context.Context, c chan<- error) {

	var err error
	err = p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.UpgradeFailed, nil, appsv1.UpgradeFailed.String())
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

func (p *UpgradingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.UpgradingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to upgradingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *UpgradingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
	deployedVersion, _, err := utils.GetDeployedReleaseVersion(actionConfig, p.manager.Spec.AppName)
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
	var chartPath string
	admin, err := kubesphere.GetAdminUsername(ctx, kubeConfig)
	if err != nil {
		klog.Errorf("get admin username failed %v", err)
		return err
	}
	if !userspace.IsSysApp(p.manager.Spec.AppName) {
		appConfig, chartPath, err = utils.GetAppConfig(ctx, p.manager.Spec.AppName, p.manager.Spec.AppOwner, cfgURL, repoURL, version, token, admin)
		if err != nil {
			klog.Errorf("get app config failed %v", err)
			return err
		}
	} else {
		chartPath, err = utils.GetIndexAndDownloadChart(ctx, p.manager.Spec.AppName, repoURL, version, token)
		if err != nil {
			klog.Errorf("download chart failed %v", err)
			return err
		}
		appConfig = &appcfg.ApplicationConfig{
			AppName:    p.manager.Spec.AppName,
			Namespace:  p.manager.Spec.AppNamespace,
			OwnerName:  p.manager.Spec.AppOwner,
			ChartsName: chartPath,
			RepoURL:    repoURL,
		}
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appConfig, token, appinstaller.Opt{Source: p.manager.Spec.Source})
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
