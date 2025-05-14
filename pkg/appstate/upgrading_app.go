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
	"errors"
	"fmt"
	"helm.sh/helm/v3/pkg/action"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

func (p *UpgradingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *UpgradingApp) IsAppCreated() bool {
	return true
}

func (p *UpgradingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UpgradingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *UpgradingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUpgradingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NewErrorUnknownState(nil, nil)
		}
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &UpgradingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
		imageClient: images.NewImageManager(client),
	}, nil
}

func (p *UpgradingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	var version string
	var actionConfig *action.Configuration
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		c <- err
		return
	}
	err = utils.UpdateAppState(ctx, p.manager, appsv1.Upgrading.String())
	if err != nil {
		c <- err
		return
	}

	actionConfig, _, err = helm.InitConfig(kubeConfig, p.manager.Spec.AppNamespace)
	if err != nil {
		c <- err
		return
	}

	defer func() {
		state := appsv1.Initializing
		message := "app enter initializing from upgrading"
		if err != nil {
			state = appsv1.UpgradeFailed
			message = fmt.Sprintf("upgrade app %s failed %v", p.manager.Spec.AppName, err)
		}
		updateErr := p.updateStatus(ctx, p.manager, state, nil, message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		c <- err
		return
	}()
	var appConfig *appcfg.ApplicationConfig

	deployedVersion, _, err := utils.GetDeployedReleaseVersion(actionConfig, p.manager.Spec.AppName)
	if err != nil {
		klog.Errorf("Failed to get release revision err=%v", err)
	}

	if !utils.MatchVersion(version, ">= "+deployedVersion) {
		c <- errors.New("upgrade version should great than deployed version")
		return
	}

	payload := p.manager.Status.Payload
	version = payload["version"]
	cfgURL := payload["cfgURL"]
	repoURL := payload["repoURL"]
	token := payload["token"]
	var chartPath string

	admin, err := kubesphere.GetAdminUsername(ctx, kubeConfig)
	if err != nil {
		c <- err
		return
	}
	if !userspace.IsSysApp(p.manager.Spec.AppName) {
		appConfig, chartPath, err = utils.GetAppConfig(ctx, p.manager.Spec.AppName, p.manager.Spec.AppOwner, cfgURL, repoURL, version, token, admin)
		if err != nil {
			c <- err
			return
		}
	} else {
		chartPath, err = utils.GetIndexAndDownloadChart(ctx, p.manager.Spec.AppName, repoURL, version, token)
		if err != nil {
			c <- err
			return
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
		c <- err
		return
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
		c <- err
		return
	}
	err = p.imageClient.Create(ctx, p.manager, refs)
	if err != nil {
		klog.Errorf("create imagemanager failed %v", err)
		c <- err
		return
	}
	err = p.imageClient.PollDownloadProgress(ctx, p.manager)
	if err != nil {
		klog.Errorf("poll image download progress failed %v", err)
		c <- err
		return
	}
	err = ops.Upgrade()
	if err != nil {
		klog.Errorf("upgrade app %s failed %v", p.manager.Spec.AppName, err)
		c <- err
		return
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
		c <- err
	case <-done:
		return
	}
}
