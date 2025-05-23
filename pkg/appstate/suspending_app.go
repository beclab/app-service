package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &SuspendingApp{}

type SuspendingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *SuspendingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *SuspendingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewSuspendingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &SuspendingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *SuspendingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	err = p.exec(ctx)
	if err != nil {
		updateErr := p.updateStatus(ctx, p.manager, appsv1.StopFailed, nil, appsv1.StopFailed.String())
		if updateErr != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.StopFailed, err)
			err = errors.Wrapf(err, "update status failed %v", updateErr)
		}
		c <- err
		return
	}
	//message := fmt.Sprintf(constants.SuspendOperationCompletedTpl, p.manager.Spec.AppName)
	//opRecord := MakeRecord(appsv1.StopOp, p.manager.Spec.Source, p.manager.Status.Payload["version"], appsv1.Stopped, message)
	updateErr := p.updateStatus(ctx, p.manager, appsv1.Stopped, nil, appsv1.Stopped.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopped.String(), err)

		err = errors.Wrapf(err, "update status failed %v", updateErr)
	}
	c <- err
	return
}
func (p *SuspendingApp) exec(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		klog.Errorf("suspend app %s failed %v", p.manager.Spec.AppName, err)
		return fmt.Errorf("suspend app %s failed %w", p.manager.Spec.AppName, err)
	}
	return nil
}

func (p *SuspendingApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *SuspendingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
