package constants

import (
	"flag"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	KubeSphereAPIScheme           = "http"
	AuthorizationTokenKey         = "X-Authorization"
	ApplicationNameLabel          = "applications.app.bytetrade.io/name"
	ApplicationAppGroupLabel      = "applications.app.bytetrade.io/group"
	ApplicationAuthorLabel        = "applications.app.bytetrade.io/author"
	ApplicationOwnerLabel         = "applications.app.bytetrade.io/owner"
	ApplicationIconLabel          = "applications.app.bytetrade.io/icon"
	ApplicationEntrancesKey       = "applications.app.bytetrade.io/entrances"
	ApplicationPortsKey           = "applications.app.bytetrade.io/ports"
	ApplicationSystemServiceLabel = "applications.app.bytetrade.io/system_service"
	ApplicationTitleLabel         = "applications.app.bytetrade.io/title"
	ApplicationImageLabel         = "applications.app.bytetrade.io/images"
	ApplicationTargetLabel        = "applications.app.bytetrade.io/target"
	ApplicationRunAsUserLabel     = "applications.apps.bytetrade.io/runasuser"
	ApplicationVersionLabel       = "applications.app.bytetrade.io/version"
	ApplicationSourceLabel        = "applications.app.bytetrade.io/source"
	ApplicationTailScaleKey       = "applications.app.bytetrade.io/tailscale"
	ApplicationRequiredGPU        = "applications.app.bytetrade.io/required_gpu"
	ApplicationPolicies           = "applications.app.bytetrade.io/policies"
	ApplicationMobileSupported    = "applications.app.bytetrade.io/mobile_supported"
	ApplicationClusterDep         = "applications.app.bytetrade.io/need_cluster_scoped_app"
	ApplicationGroupClusterDep    = "applications.app.bytetrade.io/need_cluster_scoped_group"
	UserContextAttribute          = "username"
	KubeSphereClientAttribute     = "ksclient"
	MarketSource                  = "X-Market-Source"
	MarketUser                    = "X-Market-User"
	ApplicationInstallUserLabel   = "applications.app.bytetrade.io/install_user"

	InstanceIDLabel         = "workflows.argoproj.io/controller-instanceid"
	WorkflowOwnerLabel      = "workflows.app.bytetrade.io/owner"
	WorkflowNameLabel       = "workflows.app.bytetrade.io/name"
	WorkflowTitleAnnotation = "workflows.app.bytetrade.io/title"

	OwnerNamespacePrefix = "user-space"
	OwnerNamespaceTempl  = "%s-%s"
	UserSpaceDirPVC      = "userspace-dir"

	UserAppDataDirPVC = "appcache-dir"

	UserChartsPath = "./userapps"

	EnvoyUID                        int64 = 1555
	DefaultEnvoyLogLevel                  = "debug"
	EnvoyImageVersion                     = "bytetrade/envoy:v1.25.11.1"
	EnvoyContainerName                    = "terminus-envoy-sidecar"
	EnvoyAdminPort                        = 15000
	EnvoyAdminPortName                    = "proxy-admin"
	EnvoyInboundListenerPort              = 15003
	EnvoyInboundListenerPortName          = "proxy-inbound"
	EnvoyOutboundListenerPort             = 15001
	EnvoyOutboundListenerPortName         = "proxy-outbound"
	EnvoyLivenessProbePort                = 15008
	EnvoyConfigFileName                   = "envoy.yaml"
	EnvoyConfigFilePath                   = "/config"
	EnvoyConfigOnlyOutBoundFileName       = "envoy2.yaml"
	WsContainerName                       = "terminus-ws-sidecar"
	WsContainerImage                      = "WS_CONTAINER_IMAGE"

	UploadContainerName  = "terminus-upload-sidecar"
	UploadContainerImage = "UPLOAD_CONTAINER_IMAGE"

	SidecarConfigMapVolumeName = "terminus-sidecar-config"
	SidecarInitContainerName   = "terminus-sidecar-init"
	EnvoyConfigWorkDirName     = "envoy-config"

	ByteTradeAuthor         = "bytetrade.io"
	NvshareGPU              = "nvshare.com/gpu"
	NvidiaGPU               = "nvidia.com/gpu"
	VirtAiTechVGPU          = "virtaitech.com/gpu"
	PatchOpAdd              = "add"
	PatchOpReplace          = "replace"
	EnvNvshareManagedMemory = "NVSHARE_MANAGED_MEMORY"

	AuthorizationLevelOfPublic  = "public"
	AuthorizationLevelOfPrivate = "private"

	DependencyTypeSystem = "system"
	DependencyTypeApp    = "application"
	AppDataDirURL        = "http://files-service.os-framework/api/resources/cache/%s/"

	UserSpaceDirKey   = "userspace_hostpath"
	UserAppDataDirKey = "appcache_hostpath"

	OIDCSecret = "oidc-secret"
)

var (
	empty = sets.Empty{}
	// States represents the state for whole application lifecycle.
	States = sets.String{"pending": empty, "downloading": empty, "installing": empty, "initializing": empty, "running": empty,
		"uninstalling": empty, "upgrading": empty, "suspend": empty, "resuming": empty}
	// Sources represents the source of the application.
	Sources = sets.String{"market": empty, "custom": empty, "devbox": empty, "system": empty, "unknown": empty}
	// ResourceTypes represents the type of application system supported.
	ResourceTypes = sets.String{"app": empty, "recommend": empty, "model": empty, "agent": empty, "middleware": empty}
)

var (
	// APIServerListenAddress server address for api server.
	APIServerListenAddress = ":6755"
	// WebhookServerListenAddress server address for webhook server.
	WebhookServerListenAddress = ":8433"
	// KubeSphereAPIHost kubesphere api host.
	KubeSphereAPIHost string
)

func init() {
	flag.StringVar(&APIServerListenAddress, "listen", ":6755",
		"app-service listening address")
	flag.StringVar(&WebhookServerListenAddress, "webhook-listen", ":8433",
		"webhook listening address")
	flag.StringVar(&KubeSphereAPIHost, "ks-apiserver", "ks-apiserver.kubesphere-system",
		"kubesphere api server")
}
