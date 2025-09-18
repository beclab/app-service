package appstate

import (
	"context"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &ApplyingEnvCancelFailedApp{}

type ApplyingEnvCancelFailedApp struct {
	*baseStatefulApp
}

func NewApplyingEnvCancelFailedApp(c client.Client,
	manager *appsv1.ApplicationManager) (StatefulApp, StateError) {

	return &ApplyingEnvCancelFailedApp{
		baseStatefulApp: &baseStatefulApp{
			manager: manager,
			client:  c,
		},
	}, nil
}

func (p *ApplyingEnvCancelFailedApp) Exec(ctx context.Context) (StatefulInProgressApp, error) {
	// CancelFailed 状态不需要执行操作，等待用户手动处理
	return nil, nil
}

func (p *ApplyingEnvCancelFailedApp) IsTimeout() bool {
	return false
}

func (p *ApplyingEnvCancelFailedApp) Cancel(ctx context.Context) error {
	// 在 CancelFailed 状态下，可以重试取消操作
	return nil
}
