package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"encoding/json"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &DownloadingApp{}

type DownloadingApp struct {
	StatefulApp
	baseStatefulApp
	imageClient images.ImageManager
	Lock        sync.Mutex
}

func (p *DownloadingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *DownloadingApp) IsAppCreated() bool {
	return false
}

func (p *DownloadingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *DownloadingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewDownloadingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &DownloadingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
		imageClient: images.NewImageManager(client),
	}, nil
}

func (p *DownloadingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	defer func() {
		if err != nil {
			err = p.updateStatus(context.TODO(), p.manager, appsv1.DownloadFailed, nil, appsv1.DownloadFailed.String())
			if err != nil {
				klog.Errorf("update appmgr status err %v", err)
			}
		} else {
			err = p.updateStatus(context.TODO(), p.manager, appsv1.Installing, nil, "")
			if err != nil {
				klog.Errorf("update appmgr status err %v", err)
			}
		}
	}()
	var appConfig *appcfg.ApplicationConfig
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		c <- err
		return
	}

	err = json.Unmarshal([]byte(p.manager.Spec.Config), &appConfig)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		c <- err
		return
	}
	admin, err := kubesphere.GetAdminUsername(ctx, kubeConfig)
	if err != nil {
		klog.Errorf("get admin username failed %v", admin)
		c <- err
		return
	}
	values := map[string]interface{}{
		"admin": admin,
		"bfl": map[string]string{
			"username": p.manager.Spec.AppOwner,
		},
	}
	refs, err := utils.GetRefFromResourceList(appConfig.ChartsName, values)
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
	c <- err
	return
}

func (p *DownloadingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}

func (p *DownloadingApp) Cancel(ctx context.Context) error {
	err := p.imageClient.UpdateStatus(ctx, p.manager.Name, appsv1.DownloadingCanceled.String(), appsv1.DownloadingCanceled.String())
	if err != nil {
		klog.Errorf("update im name=%s to downloadingCanceled state failed %v", err)
		return err
	}

	err = p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr name=%s to downloadingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *DownloadingApp) pollDownloadProgress(ctx context.Context) error {
	return p.imageClient.PollDownloadProgress(ctx, p.manager)
}
