package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DownloadingCancelingApp{}

type DownloadingCancelingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *DownloadingCancelingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *DownloadingCancelingApp) IsAppCreated() bool {
	return false
}

func (p *DownloadingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingCancelingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *DownloadingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewDownloadingCancelingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application failed %v", err)
		return nil, NewStateError(err.Error())
	}

	return &DownloadingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *DownloadingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	defer func() {
		DelCancelFunc(p.manager.Name)
	}()
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel downloading not found")
	}

	updateErr := p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceled, nil, appsv1.DownloadingCanceled.String())
	if updateErr != nil {
		klog.Errorf("update appmgr state to downloadingCanceled state failed %v", updateErr)
		c <- updateErr
		return
	}
	c <- nil
	return
}

func (p *DownloadingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}

func (p *DownloadingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}
