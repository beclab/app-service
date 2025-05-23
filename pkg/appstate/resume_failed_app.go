package appstate

import (
	"context"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &ResumeFailedApp{}

type ResumeFailedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *ResumeFailedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *ResumeFailedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewResumeFailedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &ResumeFailedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *ResumeFailedApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	if err != nil {
		klog.Errorf("resume-failed-app %s execute state reconcile failed %v", p.manager.Name, err)
	}
	c <- err
	return
}

func (p *ResumeFailedApp) StateReconcile(ctx context.Context) error {
	err := suspendOrResumeApp(ctx, p.client, p.manager, int32(0))
	if err != nil {
		klog.Errorf("resume-failed-app %s state reconcile failed %v", p.manager.Spec.AppName, err)
	}

	return err
}

func (p *ResumeFailedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *ResumeFailedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
