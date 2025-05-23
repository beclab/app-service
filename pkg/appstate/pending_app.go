package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &PendingApp{}

type PendingApp struct {
	StatefulApp
	baseStatefulApp
	ctx context.Context
}

func (p *PendingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *PendingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewPendingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &PendingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *PendingApp) Exec(ctx context.Context, c chan<- error) {
	var err error
	var am appsv1.ApplicationManager
	err = p.client.Get(ctx, types.NamespacedName{Name: p.manager.Name}, &am)
	if err != nil {
		klog.Errorf("get application manager failed %v", err)
		c <- err
		return
	}
	err = p.updateStatus(ctx, &am, appsv1.Downloading, nil, appsv1.Downloading.String())
	if err != nil {
		klog.Errorf("update appmgr name=%s state to downloading state failed %v", p.manager.Name, err)
		c <- err
		return
	}
	c <- err
	return
}

func (p *PendingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(ctx, p.manager, appsv1.PendingCanceling, nil, constants.OperationCanceledByUserTpl)
	if err != nil {
		klog.Errorf("update appmgr name=%s state to pendingCanceled state failed %v", p.manager.Name, err)
	}
	return err
}

func (p *PendingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
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
