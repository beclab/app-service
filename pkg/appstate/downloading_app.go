package appstate

import (
	"context"
	"encoding/json"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/images"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DownloadingApp{}
var _ PollableStatefulInProgressApp = &downloadingInProgressApp{}

type downloadingInProgressApp struct {
	*DownloadingApp
	cancelPoll context.CancelFunc
	ctxPoll    context.Context
}

// Cleanup implements PollableStatefulInProgressApp.
func (r *downloadingInProgressApp) Cleanup(ctx context.Context) {
	r.stopPolling()
}

func (r *downloadingInProgressApp) stopPolling() {
	r.cancelPoll()
}

func (r *downloadingInProgressApp) poll(ctx context.Context) error {
	pollCtx, cancel := context.WithCancel(ctx)
	r.cancelPoll = cancel
	r.ctxPoll = pollCtx
	return r.imageClient.PollDownloadProgress(pollCtx, r.manager)
}

func (r *downloadingInProgressApp) WaitAsync(ctx context.Context) {
	appFactory.waitForPolling(ctx, r, func() {
		updateErr := r.updateStatus(ctx, r.manager, appsv1.Installing, nil, appsv1.Installing.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", r.manager.Name, appsv1.Installing.String(), updateErr)
		}
	})
}

// override
func (r *downloadingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	// do nothing here
	// do not exec duplicately
	return nil, nil
}

func (p *downloadingInProgressApp) Done() <-chan struct{} {
	if p.ctxPoll == nil {
		return nil
	}

	return p.ctxPoll.Done()
}

func (p *downloadingInProgressApp) Cancel(ctx context.Context) error {
	klog.Infof("call downloadingApp cancel....")
	err := p.imageClient.UpdateStatus(ctx, p.manager.Name, appsv1.DownloadingCanceled.String(), appsv1.DownloadingCanceled.String())
	if err != nil {
		klog.Errorf("update im name=%s to downloadingCanceled state failed %v", err)
		return err
	}

	err = p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update app manager name=%s to downloadingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *downloadingInProgressApp) IsTimeout() bool {
	if p.ttl == 0 {
		return false
	}
	return p.manager.Status.StatusTime.Add(p.ttl).Before(time.Now())
}

type DownloadingApp struct {
	StatefulApp
	baseStatefulApp
	imageClient images.ImageManager
	ttl         time.Duration
}

func (p *DownloadingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func (p *DownloadingApp) IsOperation() bool {
	return true
}

func (p *DownloadingApp) IsCancelOperation() bool {
	return false
}

func (p *DownloadingApp) IsAppCreated() bool {
	return false
}

func (p *DownloadingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *DownloadingApp) IsTimeout() bool {
	if p.ttl == 0 {
		return false
	}
	return p.manager.Status.StatusTime.Add(p.ttl).Before(time.Now())
}

func NewDownloadingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {
	// TODO: check app state

	//

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &DownloadingApp{
				baseStatefulApp: baseStatefulApp{
					client:  c,
					manager: manager,
				},
				ttl:         ttl,
				imageClient: images.NewImageManager(c),
			}
		})
}

func (p *DownloadingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.exec(ctx)
	if err != nil {
		klog.Errorf("app %s downloading failed %v", p.manager.Spec.AppName, err)
		updateErr := p.updateStatus(ctx, p.manager, appsv1.DownloadFailed, nil, appsv1.DownloadFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadFailed.String(), updateErr)
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		return nil, err
	}

	return &downloadingInProgressApp{
		DownloadingApp: p,
	}, nil
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

	return nil
}
