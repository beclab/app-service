package appstate

import (
	"context"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DoNothingApp{}

type DoNothingApp struct {
	baseStatefulApp
}

func (p *DoNothingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DoNothingApp) IsOperation() bool {
	return true
}

func (p *DoNothingApp) IsCancelOperation() bool {
	return false
}

func (p *DoNothingApp) IsAppCreated() bool {
	return false
}

func (p *DoNothingApp) IsTimeout() bool {
	return false
}

func NewDoNothingApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {

			return &DoNothingApp{
				baseStatefulApp: baseStatefulApp{
					manager: manager,
					client:  c,
				},
			}
		})
}

func (p *DoNothingApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	return nil, nil
}

func (p *DoNothingApp) Cancel(ctx context.Context) error {
	return nil
}
