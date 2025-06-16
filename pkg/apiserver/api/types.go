package api

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
	UID string `json:"uid"`
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
	Dev     bool      `json:"devMode"`
	RepoURL string    `json:"repoUrl"`
	CfgURL  string    `json:"cfgUrl"`
	Source  AppSource `json:"source"`
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

// ResponseWithMsg represents  a response with an additional message.
type ResponseWithMsg struct {
	Response
	Message string `json:"message"`
}

type ImageInfoRequest struct {
	Name   string   `json:"name"`
	Images []string `json:"images"`
}
