package appstate

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ OperationApp = &ResumingApp{}

type ResumingApp struct {
	*baseOperationApp
}

func NewResumingApp(c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &ResumingApp{
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

func (p *ResumingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	err := p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.ResumeFailed, nil, appsv1.ResumeFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.ResumeFailed, err)
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		return nil, err
	}

	return &resumingInProgressApp{
		ResumingApp:                       p,
		basePollableStatefulInProgressApp: &basePollableStatefulInProgressApp{},
	}, nil
}

func (p *ResumingApp) exec(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(1))
	if err != nil {
		klog.Errorf("resume app %s failed %v", p.manager.Spec.AppName, err)
		return fmt.Errorf("resume app %s failed %w", p.manager.Spec.AppName, err)
	}
	return nil
}

var _ PollableStatefulInProgressApp = &resumingInProgressApp{}

type resumingInProgressApp struct {
	*ResumingApp
	*basePollableStatefulInProgressApp
}

// Exec implements PollableStatefulInProgressApp.
// Subtle: this method shadows the method (*ResumingApp).Exec of resumingInProgressApp.ResumingApp.
func (p *resumingInProgressApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}

// WaitAsync implements PollableStatefulInProgressApp.
func (p *resumingInProgressApp) WaitAsync(ctx context.Context) {
	appFactory.waitForPolling(ctx, p, func() {
		updateErr := p.updateStatus(context.TODO(), p.manager, appsv1.Initializing, nil, appsv1.Initializing.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Initializing.String(), updateErr)
		}
	})
}

// poll implements PollableStatefulInProgressApp.
func (p *resumingInProgressApp) poll(ctx context.Context) error {
	ok := p.IsStartUp(ctx)
	if !ok {
		return fmt.Errorf("wait for app %s startup failed", p.manager.Spec.AppName)
	}

	return nil
}

func (p *resumingInProgressApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.ResumingCanceling, nil, constants.OperationCanceledByTerminusTpl)
	if err != nil {
		klog.Errorf("update appmgr state to resumingCanceling state failed %v", err)
		return err
	}
	return nil
}

func (p *resumingInProgressApp) IsStartUp(ctx context.Context) bool {
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
