package appstate

import (
	"context"
	"fmt"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &ResumingApp{}

type ResumingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *ResumingApp) IsOperating() bool {
	curState := p.manager.Status.State
	return OperatingStates[curState]
}

func (p *ResumingApp) IsAppCreated() bool {
	return true
}

func (p *ResumingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *ResumingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *ResumingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewResumingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NewErrorUnknownState(nil, nil)
		}
		klog.Error("get application error: ", err)
		return nil, NewStateError(err.Error())
	}

	return &ResumingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			app:     &app,
			client:  client,
		},
	}, nil
}

func (p *ResumingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	defer func() {
		state := appsv1.Initializing
		message := "from resuming to initializing"
		if err != nil {
			state = appsv1.ResumeFailed
			message = fmt.Sprintf("resume app %s failed %v", p.manager.Spec.AppName, err)
		}
		updateErr := p.updateStatus(context.TODO(), p.manager, state, nil, message)
		if err == nil && updateErr != nil {
			err = updateErr
		}
		c <- err
		return
	}()
	err = suspendOrResumeApp(ctx, p.client, p.manager, int32(1))
	if err != nil {

		c <- fmt.Errorf("resume app %s failed %w", p.manager.Spec.AppOwner, err)
		return
	}
	ok := p.IsStartUp(ctx)
	if !ok {
		c <- fmt.Errorf("wait for app %s startup failed", p.manager.Spec.AppName)
	}
	c <- nil
	return
}

func (p *ResumingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.ResumingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to resumingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *ResumingApp) IsStartUp(ctx context.Context) bool {
	timer := time.NewTicker(time.Second)

	for {
		select {
		case <-timer.C:
			startedUp, _ := isStartUp(p.manager)
			if startedUp {
				klog.Infof("time: %v, appState: %v", time.Now(), appsv1.AppInitializing)
				return true
			}
		case <-ctx.Done():
			klog.Infof("Waiting for app startup canceled appName=%s", p.manager.Spec.AppName)
			return false
		}
	}
}

func (p *ResumingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		c <- err
	case <-done:
		return
	}
}
