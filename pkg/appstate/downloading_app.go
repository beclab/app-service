package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DownloadingApp{}

type DownloadingApp struct {
	StatefulApp
	baseStatefulApp
	imageClient images.ImageManager
	//Lock        sync.Mutex
}

func (p *DownloadingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewDownloadingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &DownloadingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
		imageClient: images.NewImageManager(client),
	}
}

func (p *DownloadingApp) Exec(ctx context.Context, c chan<- error) {
	err := p.exec(ctx)
	klog.Errorf("download exec return : %v", err)
	if err != nil && !errors.Is(err, context.Canceled) {
		klog.Errorf("app %s downloading failed %v", p.manager.Spec.AppName, err)
		updateErr := p.updateStatus(ctx, p.manager, appsv1.DownloadFailed, nil, appsv1.DownloadFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadFailed.String(), updateErr)
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		c <- err
		return
	}
	updateErr := p.updateStatus(ctx, p.manager, appsv1.Installing, nil, appsv1.Installing.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Installing.String(), updateErr)
		err = errors.Wrapf(err, "update status failed %v", updateErr)
	}
	c <- err
	return
}

func (p *DownloadingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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

func (p *DownloadingApp) Cancel(ctx context.Context) error {
	klog.Infof("call downloadingApp cancel....")
	err := p.imageClient.UpdateStatus(ctx, p.manager.Name, appsv1.DownloadingCanceled.String(), appsv1.DownloadingCanceled.String())
	if err != nil {
		klog.Errorf("update im name=%s to downloadingCanceled state failed %v", err)
		return err
	}

	//err = p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	//if err != nil {
	//	klog.Errorf("update app manager name=%s to downloadingCanceling state failed %v", err)
	//	return err
	//}
	return nil
}

func (p *DownloadingApp) pollDownloadProgress(ctx context.Context) error {
	return p.imageClient.PollDownloadProgress(ctx, p.manager)
}

func (p *DownloadingApp) exec(ctx context.Context) error {
	var err error
	var appConfig *appcfg.ApplicationConfig
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	err = json.Unmarshal([]byte(p.manager.Spec.Config), &appConfig)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		return err
	}
	admin, err := kubesphere.GetAdminUsername(ctx, kubeConfig)
	if err != nil {
		klog.Errorf("get admin username failed %v", admin)
		return err
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
	return nil
}
