package apiserver

import (
	"context"

	"bytetrade.io/web3os/app-service/pkg/upgrade"
	"bytetrade.io/web3os/app-service/pkg/users/userspace/v1"
	"bytetrade.io/web3os/app-service/pkg/webhook"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Handler include several fields that used for managing interactions with associated services.
type Handler struct {
	kubeHost         string
	serviceCtx       context.Context
	userspaceManager *userspace.Manager
	kubeConfig       *rest.Config // helm's kubeConfig. TODO: insecure
	sidecarWebhook   *webhook.Webhook
	upgrader         *upgrade.Upgrader
	ctrlClient       client.Client
}

type handlerBuilder struct {
	ctx        context.Context
	ksHost     string
	kubeConfig *rest.Config
	ctrlClient client.Client
}

func (b *handlerBuilder) WithKubesphereConfig(ksHost string) *handlerBuilder {
	b.ksHost = ksHost
	return b
}

func (b *handlerBuilder) WithContext(ctx context.Context) *handlerBuilder {
	b.ctx = ctx
	return b
}

func (b *handlerBuilder) WithKubernetesConfig(config *rest.Config) *handlerBuilder {
	b.kubeConfig = config
	return b
}

func (b *handlerBuilder) WithCtrlClient(client client.Client) *handlerBuilder {
	b.ctrlClient = client
	return b
}

func (b *handlerBuilder) Build() (*Handler, error) {
	wh, err := webhook.New(b.kubeConfig)
	if err != nil {
		return nil, err
	}

	err = wh.CreateOrUpdateSandboxMutatingWebhook()
	if err != nil {
		return nil, err
	}
	err = wh.CreateOrUpdateAppNamespaceValidatingWebhook()
	if err != nil {
		return nil, err
	}
	err = wh.CreateOrUpdateGpuLimitMutatingWebhook()
	if err != nil {
		return nil, err
	}
	err = wh.CreateOrUpdateProviderRegistryValidatingWebhook()
	if err != nil {
		return nil, err
	}
	err = wh.CreateOrUpdateEvictionValidatingWebhook()
	if err != nil {
		return nil, err
	}

	return &Handler{
		kubeHost:         b.ksHost,
		serviceCtx:       b.ctx,
		kubeConfig:       b.kubeConfig,
		userspaceManager: userspace.NewManager(b.ctx),
		sidecarWebhook:   wh,
		upgrader:         upgrade.NewUpgrader(),
		ctrlClient:       b.ctrlClient,
	}, err

}
