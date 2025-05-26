package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	k8sappv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

func (p *PendingApp) IsOperation() bool {
	return true
}

func (p *PendingApp) IsCancelOperation() bool {
	return false
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

func NewPendingApp(ctx context.Context, c client.Client,
	manager *appsv1.ApplicationManager, ttl time.Duration) (StatefulApp, StateError) {

	// Application's meta.name == ApplicationMannager's meta.name
	var app appsv1.Application
	err := c.Get(ctx, types.NamespacedName{Name: manager.Name}, &app)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Error("get application error: ", err)
		return nil, NewStateError(err.Error())
	}

	// manager of pending state, application is not created yet
	if err == nil {
		return nil, NewErrorUnknownState(
			func() func(ctx context.Context) error {
				return func(ctx context.Context) error {
					return removeUnknownApplication(c, manager.Name)(ctx)
				}
			},
			nil, // TODO: clean up, delete all, application and application manager
		)
	}

	return appFactory.New(c, manager, ttl,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &PendingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
				ttl: ttl,
			}
		})
}

func (p *PendingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {

	p.manager.Status.State = appsv1.Downloading
	now := metav1.Now()
	p.manager.Status.StatusTime = &now
	p.manager.Status.UpdateTime = &now
	err := p.client.Status().Update(ctx, p.manager)
	if err != nil {
		klog.Error("update app manager status error, ", err, ", ", p.manager.Name)
	}

	return nil, err
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

		// delete the whole namespace if the namespace is not system namespace
		if !utils.IsProtectedNamespace(app.Spec.Namespace) {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: app.Spec.Namespace,
				},
			}

			// application will be removed automatically when the ns is removed
			err = client.Delete(ctx, &ns)
			if err != nil {
				klog.Error("delete namespace error: ", err, ", ", app.Spec.Namespace)
				return err
			}

		} else {
			// delete the deployment of application
			var deploy k8sappv1.Deployment
			if err := client.Get(ctx, types.NamespacedName{Name: app.Spec.Name}, &deploy); err == nil {
				klog.Info("delete deployment of application: %s", app.Spec.Name)
				if err = client.Delete(ctx, &deploy); err != nil {
					klog.Error("delete deployment error: ", err, ", ", name)
				}
			} else {
				var sts k8sappv1.StatefulSet
				if err := client.Get(ctx, types.NamespacedName{Name: name}, &sts); err == nil {
					klog.Info("delete statefulset of application: %s", name)
					if err = client.Delete(ctx, &sts); err != nil {
						klog.Error("delete statefulset error: ", err, ", ", name)
					}
				}
			}
		}

		return nil
	}
}
