package upgrade

import (
	"context"
	"net/http"

	"github.com/hashicorp/go-version"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PipelineContext struct {
	CurrentVersion    *version.Version
	ReleaseVersion    *version.Version
	InstallPackgePath string
	InstallCtx        context.Context
	CancelInstall     context.CancelFunc
	K8sClient         client.Client
	httpClient        *http.Client
}

func NewCtx(backgroundCtx context.Context) *PipelineContext {
	ctx, cancel := context.WithCancel(backgroundCtx)
	return &PipelineContext{
		InstallCtx:    ctx,
		CancelInstall: cancel,
	}
}
