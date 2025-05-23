package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &SuspendedApp{}

type SuspendedApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *SuspendedApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *SuspendedApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewSuspendedApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &SuspendedApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *SuspendedApp) Exec(ctx context.Context, c chan<- error) {
	c <- nil
	return
}

func (p *SuspendedApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *SuspendedApp) StateReconcile(ctx context.Context) error {
	var err error
	var app appsv1.Application
	err = p.client.Get(ctx, types.NamespacedName{Name: p.manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		err = p.forceDeleteApp(ctx)
		if err != nil {
			klog.Errorf("delete app %s failed %s", p.manager.Spec.AppName, err)
			return err
		}
		return nil
	}
	if app.Status.State != appsv1.AppStopped.String() {
		err = p.updateStatus(ctx, p.manager, appsv1.Stopping, nil, appsv1.Stopping.String())
		if err != nil {
			klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.Stopping, err)
			return err
		}
	}
	return nil
}

func (p *SuspendedApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
