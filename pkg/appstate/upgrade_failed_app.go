package appstate

import (
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ OperationApp = &UpgradeFailedApp{}

type UpgradeFailedApp struct {
	SuspendFailedApp
}

func NewUpgradeFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {
	return appFactory.New(c, manager, 0,
		func(c client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp {
			return &UpgradeFailedApp{
				SuspendFailedApp: SuspendFailedApp{
					&baseOperationApp{
						ttl: ttl,
						baseStatefulApp: &baseStatefulApp{
							manager: manager,
							client:  c,
						},
					},
				},
			}
		})
}
