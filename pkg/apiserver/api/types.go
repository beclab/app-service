package api

import (
	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
)

const (
	AppTokenKey         = "bytetrade.io/token"
	AppRepoURLKey       = "bytetrade.io/repo-url"
	AppVersionKey       = "bytetrade.io/chart-version"
	AppMarketSourceKey  = constants.AppMarketSourceKey
	AppInstallSourceKey = "bytetrade.io/install-source"
	AppUninstallAllKey  = "bytetrade.io/uninstall-all"
	AppImagesKey        = "bytetrade.io/images"
)

// Response represents the code for response.
type Response struct {
	Code int32 `json:"code"`
}

// InstallationResponse represents the response for installation.
type InstallationResponse struct {
	Response
	Data InstallationResponseData `json:"data"`
}

// InstallationResponseData represents the installation response uid.
type InstallationResponseData struct {
	UID  string `json:"uid"`
	OpID string `json:"opID"`
}

// DependenciesRespData represents the dependencies of an application.
type DependenciesRespData struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
	// dependency type: system, application.
	Type string `yaml:"type" json:"type"`
}

// DependenciesResp represents the response for application dependencies.
type DependenciesResp struct {
	Response
	Data []DependenciesRespData `json:"data"`
}

// ReleaseUpgradeResponse represents a response for a release upgrade operation.
type ReleaseUpgradeResponse struct {
	Response
	Data ReleaseUpgradeResponseData `json:"data"`
}

// ReleaseUpgradeResponseData represents a response uid for a release upgrade operation.
type ReleaseUpgradeResponseData struct {
	UID string `json:"uid"`
}

// ReleaseVersionResponse represents a response for retrieving release version.
type ReleaseVersionResponse struct {
	Response
	Data ReleaseVersionData `json:"data"`
}

// ReleaseVersionData contains release version.
type ReleaseVersionData struct {
	Version string `json:"version"`
}

type UserAppsResponse struct {
	Response
	Data UserAppsStatusRespData `json:"data"`
}

type UserAppsStatusRespData struct {
	User   string        `json:"user"`
	Status string        `json:"status"`
	Ports  UserAppsPorts `json:"ports"`
	Error  string        `json:"error"`
}

type UserAppsPorts struct {
	Desktop int32 `json:"desktop"`
	Wizard  int32 `json:"wizard"`
}

// RequirementResp represents a response for application requirement.
type RequirementResp struct {
	Response
	Resource string `json:"resource"`
	Message  string `json:"message"`
}

// AppSource describe the source of an application, recommend,model,agent
type AppSource string

const (
	// Market deployed from market.
	Market AppSource = "market"
	// Custom deployed from upload chart by user.
	Custom AppSource = "custom"
	// DevBox deployed from devbox.
	DevBox AppSource = "devbox"
	// System deployed from system.
	System AppSource = "system"
	// Unknown means the source is unknown.
	Unknown AppSource = "unknown"
)

func (as AppSource) String() string {
	return string(as)
}

// UpgradeRequest represents a request to upgrade an application.
type UpgradeRequest struct {
	CfgURL  string    `json:"cfgURL,omitempty"`
	RepoURL string    `json:"repoURL"`
	Version string    `json:"version"`
	Source  AppSource `json:"source"`
}

// InstallRequest represents a request to install an application.
type InstallRequest struct {
	Dev        bool                    `json:"devMode"`
	RepoURL    string                  `json:"repoUrl"`
	CfgURL     string                  `json:"cfgUrl"`
	Source     AppSource               `json:"source"`
	Images     []Image                 `json:"images"`
	Envs       []sysv1alpha1.AppEnvVar `json:"envs"`
	RawAppName string                  `json:"rawAppName"`
	Title      string                  `json:"title"`
	Entrances  []EntranceClone         `json:"entrances"`
}

type Image struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// UninstallRequest represents a request to uninstall an application.
type UninstallRequest struct {
	All bool `json:"all"`
}

type ManifestRenderRequest struct {
	Content string `json:"content"`
}

type ManifestRenderResponse struct {
	Response
	Data ManifestRenderRespData `json:"data"`
}
type ManifestRenderRespData struct {
	Content string `json:"content"`
}

type AdminUsernameResponse struct {
	Response
	Data AdminUsernameRespData `json:"data"`
}

type AdminUsernameRespData struct {
	Username string `json:"username"`
}

type AdminListResponse struct {
	Response
	Data []string `json:"data"`
}

// ResponseWithMsg represents  a response with an additional message.
type ResponseWithMsg struct {
	Response
	Message string `json:"message"`
}

type ImageInfoRequest struct {
	AppName string      `json:"name"`
	Images  []ImageInfo `json:"images"`
}
type ImageInfo struct {
	ImageName    string                    `json:"name"`
	ManifestList ManifestList              `json:"manifest_list"`
	ArchManifest map[string]*ImageManifest `json:"arch_manifest"`
}

type Config struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type Layer struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

type ManifestItem struct {
	MediaType string   `json:"mediaType"`
	Size      int64    `json:"size"`
	Digest    string   `json:"digest"`
	Platform  Platform `json:"platform"`
}

type ImageManifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	MediaType     string  `json:"mediaType"`
	Config        Config  `json:"config"`
	Layers        []Layer `json:"layers"`
}

type ManifestList struct {
	SchemaVersion int            `json:"schemaVersion"`
	MediaType     string         `json:"mediaType"`
	Manifests     []ManifestItem `json:"manifests"`
}

var (
	CheckTypeAppEnv      = "appenv"
	CheckTypeAppEntrance = "appEntrance"
)

type FailedCheckResponse struct {
	Code int                     `json:"code"`
	Data FailedCheckResponseData `json:"data"`
}

type FailedCheckResponseData struct {
	Type string `json:"type"`
	Data any    `json:",inline"`
}

type AppEnvCheckResult struct {
	MissingValues []sysv1alpha1.AppEnvVar `json:"missingValues"`
	MissingRefs   []sysv1alpha1.AppEnvVar `json:"missingRefs"`
	InvalidValues []sysv1alpha1.AppEnvVar `json:"invalidValues"`
}

type CanDeployResponse struct {
	Response
	Data CanDeployResponseData `json:"data"`
}

type CanDeployResponseData struct {
	CanOp bool `json:"canOp"`
}

type EntranceClone struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title"`
	Message string `json:"message,omitempty"`
}

type AppTitle struct {
	Title   string `json:"title"`
	IsValid bool   `json:"isValid"`
	Message string `json:"message"`
}

type AppEntranceCheckResult struct {
	MissingValues   []EntranceClone `json:"missingValues"`
	InvalidValues   []EntranceClone `json:"invalidValues"`
	TitleValidation AppTitle        `json:"titleValidation"`
}
