package appstate

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &ResumingApp{}

type ResumingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *ResumingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *ResumingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewResumingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &ResumingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *ResumingApp) Exec(ctx context.Context, c chan<- error) {
	err := p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.ResumeFailed, nil, appsv1.ResumeFailed.String())
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
	start := time.Now()
	for {
		select {
		case <-timer.C:
			startedUp, _ := isStartUp(p.manager, p.client)
			klog.Infof("wait app %s pod to startup, time elapsed: %v", p.manager.Spec.AppOwner, time.Since(start))
			if startedUp {
				klog.Infof("time: %v, appState: %v", time.Now(), appsv1.Initializing)
				return true
			}
		case <-ctx.Done():
			klog.Infof("Waiting for app startup canceled appName=%s", p.manager.Spec.AppName)
			return false
		}
	}
}

func (p *ResumingApp) exec(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(1))
	if err != nil {
		klog.Errorf("resume app %s failed %v", p.manager.Spec.AppName, err)
		return fmt.Errorf("resume app %s failed %w", p.manager.Spec.AppName, err)
	}
	ok := p.IsStartUp(ctx)
	if !ok {
		return fmt.Errorf("wait for app %s startup failed", p.manager.Spec.AppName)
	}
	return nil
}

func (p *ResumingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
