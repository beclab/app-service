package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/v2"
)

var _ StatefulApp = &PendingApp{}

type PendingApp struct {
	StatefulApp
	baseStatefulApp
	ttl time.Duration
}

func (p *PendingApp) IsOperating() bool {
	return true
}

func (p *PendingApp) IsAppCreated() bool {
	return false
}

func (p *PendingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *PendingApp) GetApp() *appsv1.Application {
	return p.app
}

func (p *PendingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func (p *PendingApp) IsTimeout() bool {
	if p.ttl == 0 {
		return false
	}
	return p.manager.Status.StatusTime.Add(p.ttl).Before(time.Now())
}

func NewPendingApp(ctx context.Context, client client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	// Application's meta.name == ApplicationMannager's meta.name
	var app appsv1.Application
	err := client.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Error("get application error: ", err)
		return nil, NewStateError(err.Error())
	}

	// manager of pending state, application is not created yet
	if err == nil {
		return nil, NewErrorUnknownState(
			func() func(ctx context.Context) error {
				return func(ctx context.Context) error {
					return removeUnknownApplication(client, manager.Name)(ctx)
				}
			},
			nil, // TODO: clean up, delete all, application and application manager
		)
	}

	return &PendingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
		ttl: ttl,
	}, nil
}

func (p *PendingApp) Exec(ctx context.Context) error {

	p.manager.Status.State = appsv1.Downloading
	now := metav1.Now()
	p.manager.Status.StatusTime = &now
	p.manager.Status.UpdateTime = &now
	err := p.client.Status().Update(ctx, p.manager)
	if err != nil {
		klog.Error("update app manager status error, ", err, ", ", p.manager.Name)
	}

	return err
}

func (p *PendingApp) Cancel(ctx context.Context) error {
	err := p.updateStatus(context.TODO(), p.manager, appsv1.PendingCanceled, nil, constants.OperationCanceledByUserTpl)
	if err != nil {
		klog.Info("Failed to update applicationmanagers status name=%s err=%v", p.manager.Name, err)
	}

	return err
}

func removeUnknownApplication(client client.Client, name string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		var app appsv1.Application
		err := client.Get(ctx, types.NamespacedName{Name: name}, &app)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Error("get application error: ", err)
			return err
		}

		if apierrors.IsNotFound(err) {
			return nil
		}

		err = client.Delete(ctx, &app)
		if err != nil {
			klog.Error("delete application error: ", err, ", ", name)
			return err
		}

		return nil
	}
}
