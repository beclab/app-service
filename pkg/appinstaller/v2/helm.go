package v2

import (
	"context"

	"bytetrade.io/web3os/app-service/pkg/appcfg"
	v1 "bytetrade.io/web3os/app-service/pkg/appinstaller"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

var _ v1.HelmOpsInterface = &HelmOpsV2{}

type HelmOpsV2 struct {
	*v1.HelmOps
}

func NewHelmOps(ctx context.Context, kubeConfig *rest.Config, app *appcfg.ApplicationConfig, token string, options v1.Opt) (v1.HelmOpsInterface, error) {
	v1Ops, err := v1.NewHelmOps(ctx, kubeConfig, app, token, options)
	if err != nil {
		klog.Errorf("Failed to create HelmOps: %v", err)
		return nil, err
	}

	return &HelmOpsV2{
		HelmOps: v1Ops.(*v1.HelmOps),
	}, nil
}

func (h *HelmOpsV2) Install() error {

	return nil
}
