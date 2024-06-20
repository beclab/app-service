package constants

import (
	"flag"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	KubeSphereAPIScheme           = "http"
	AuthorizationTokenKey         = "X-Authorization"
	ApplicationNameLabel          = "applications.app.bytetrade.io/name"
	ApplicationAuthorLabel        = "applications.app.bytetrade.io/author"
	ApplicationOwnerLabel         = "applications.app.bytetrade.io/owner"
	ApplicationIconLabel          = "applications.app.bytetrade.io/icon"
	ApplicationEntrancesKey       = "applications.app.bytetrade.io/entrances"
	ApplicationSystemServiceLabel = "applications.app.bytetrade.io/system_service"
	ApplicationTitleLabel         = "applications.app.bytetrade.io/title"
	ApplicationTargetLabel        = "applications.app.bytetrade.io/target"
	ApplicationVersionLabel       = "applications.app.bytetrade.io/version"
	ApplicationSourceLabel        = "applications.app.bytetrade.io/source"
	ApplicationAnalytics          = "applications.app.bytetrade.io/analytics"
	ApplicationPolicies           = "applications.app.bytetrade.io/policies"
	ApplicationMobileSupported    = "applications.app.bytetrade.io/mobile_supported"
	ApplicationClusterDep         = "applications.app.bytetrade.io/need_cluster_scoped_app"
	UserContextAttribute          = "username"
	KubeSphereClientAttribute     = "ksclient"

	InstanceIDLabel         = "workflows.argoproj.io/controller-instanceid"
	WorkflowOwnerLabel      = "workflows.app.bytetrade.io/owner"
	WorkflowNameLabel       = "workflows.app.bytetrade.io/name"
	WorkflowTitleAnnotation = "workflows.app.bytetrade.io/title"

	KubeSphereNamespace        = "kubesphere-system"
	KubeSphereConfigName       = "kubesphere-config"
	KubeSphereConfigMapDataKey = "kubesphere.yaml"

	OwnerNamespacePrefix = "user-space"
	OwnerNamespaceTempl  = "%s-%s"
	UserSpaceDirPVC      = "userspace-dir"

	UserAppDataDirPVC = "appcache-dir"

	UserChartsPath = "./userapps"

	EnvoyUID                      int64 = 1000
	DefaultEnvoyLogLevel                = "info"
	EnvoyImageVersion                   = "envoyproxy/envoy-distroless:v1.25.2"
	EnvoyContainerName                  = "terminus-envoy-sidecar"
	EnvoyAdminPort                      = 15000
	EnvoyAdminPortName                  = "proxy-admin"
	EnvoyInboundListenerPort            = 15003
	EnvoyInboundListenerPortName        = "proxy-inbound"
	EnvoyOutboundListenerPort           = 15001
	EnvoyOutboundListenerPortName       = "proxy-outbound"
	EnvoyLivenessProbePort              = 15008
	EnvoyConfigFileName                 = "envoy.yaml"
	EnvoyConfigFilePath                 = "/etc/envoy"

	WsContainerName  = "terminus-ws-sidecar"
	WsContainerImage = "WS_CONTAINER_IMAGE"

	UploadContainerName  = "terminus-upload-sidecar"
	UploadContainerImage = "UPLOAD_CONTAINER_IMAGE"

	SidecarConfigMapVolumeName = "terminus-sidecar-config"
	SidecarInitContainerName   = "terminus-sidecar-init"

	ByteTradeAuthor  = "bytetrade.io"
	EnvOrionVGPU     = "ORION_VGPU"
	EnvOrionClientID = "ORION_CLIENT_ID"
	EnvOrionTaskName = "ORION_TASK_NAME"
	EnvOrionGMEM     = "ORION_GMEM"
	EnvOrionReserved = "ORION_RESERVED"
	NvshareGPU       = "nvshare.com/gpu"
	NvidiaGPU        = "nvidia.com/gpu"
	VirtAiTechVGPU   = "virtaitech.com/gpu"
	PatchOpAdd       = "add"
	PatchOpReplace   = "replace"

	AuthorizationLevelOfPublic  = "public"
	AuthorizationLevelOfPrivate = "private"

	DependencyTypeSystem = "system"
	DependencyTypeApp    = "application"
	AppDataDirURL        = "http://files-service.user-space-%s/api/resources/AppData/%s"

	UserSpaceDirKey   = "userspace_hostpath"
	UserAppDataDirKey = "appcache_hostpath"
)

var (
	empty = sets.Empty{}
	// States represents the state for whole application lifecycle.
	States = sets.String{"pending": empty, "installing": empty, "running": empty,
		"uninstalling": empty, "upgrading": empty, "suspend": empty}
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
