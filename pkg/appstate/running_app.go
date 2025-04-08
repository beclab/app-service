package appstate

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &RunningApp{}

type RunningApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *RunningApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *RunningApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewRunningApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {
	return &RunningApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *RunningApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	c <- err
	return
}

func (p *RunningApp) StateReconcile(ctx context.Context) error {
	var app appsv1.Application
	err := p.client.Get(ctx, types.NamespacedName{Name: p.manager.Name}, &app)

	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("get application %s failed %v", p.manager.Name, err)
		return err
	}
	if apierrors.IsNotFound(err) {
		err = p.forceDeleteApp(ctx)
		if err != nil {
			klog.Errorf("delete app %s failed %v", p.manager.Spec.AppName, err)
			return err
		}
		return nil
	}
	return nil
}

func (p *RunningApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *RunningApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
