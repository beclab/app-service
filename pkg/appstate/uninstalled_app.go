package appstate

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &UninstalledApp{}

type UninstalledApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *UninstalledApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *UninstalledApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewUninstalledApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &UninstalledApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *UninstalledApp) Exec(ctx context.Context, c chan<- error) {
	err := p.StateReconcile(ctx)
	c <- err
	return
}

func (p *UninstalledApp) StateReconcile(ctx context.Context) error {
	var err error
	var app appsv1.Application
	err = p.client.Get(ctx, types.NamespacedName{Name: p.manager.Name}, &app)

	if apierrors.IsNotFound(err) {
		return nil
	}

	// app is not expected to exist
	if err == nil {
		err = p.forceDeleteApp(ctx)
		if err != nil {
			klog.Infof("delete app %s failed %v", p.manager.Spec.AppName, err)
			return err
		}
		return nil
	}
	return err
}

func (p *UninstalledApp) Cancel(ctx context.Context) error {
	return nil
}

func (p *UninstalledApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		if err != nil {
			klog.Errorf("app manager %s cancel failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}
