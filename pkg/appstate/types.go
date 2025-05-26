package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulApp interface {
	GetApp() *appsv1.Application
	GetManager() *appsv1.ApplicationManager
	IsOperation() bool
	IsCancelOperation() bool
	IsAppCreated() bool
	State() string
	IsTimeout() bool
	Exec(ctx context.Context) (StatefulInProgressApp, error)
}

type baseStatefulApp struct {
	app     *appsv1.Application
	manager *appsv1.ApplicationManager
	client  client.Client
}

func (b *baseStatefulApp) GetManager() *appsv1.ApplicationManager {
	return b.manager
}

func (b *baseStatefulApp) GetApp() *appsv1.Application {
	return b.app
}

func (b *baseStatefulApp) updateStatus(ctx context.Context, am *appsv1.ApplicationManager, state appsv1.ApplicationManagerState,
	opRecord *appsv1.OpRecord, message string) error {
	var err error
	appState := ""
	if appsv1.AppStateCollect.Has(am.Status.State.String()) {
		appState = am.Status.State.String()
	}
	err = b.client.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		return err
	}

	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.Message = message
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	if opRecord != nil {
		amCopy.Status.OpRecords = append([]appsv1.OpRecord{*opRecord}, amCopy.Status.OpRecords...)
	}
	if len(amCopy.Status.OpRecords) > 20 {
		amCopy.Status.OpRecords = amCopy.Status.OpRecords[:20:20]
	}
	err = b.client.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		return err
	}
	var aa appsv1.ApplicationManager
	err = b.client.Get(ctx, types.NamespacedName{Name: am.Name}, &aa)
	if err != nil {
		return err
	}
	if len(appState) > 0 {
		err = utils.UpdateAppState(ctx, am, appState)
		if err != nil {
			return err
		}
	}
	return nil
}

type StatefulInProgressApp interface {
	StatefulApp
	Cancel(ctx context.Context) error
	Cleanup(ctx context.Context)
	Done() <-chan struct{}
}

type PollableStatefulInProgressApp interface {
	StatefulInProgressApp
	poll(ctx context.Context) error
	stopPolling()
	WaitAsync(ctx context.Context)
}
