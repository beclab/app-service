package appinstaller

import (
	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/tapr"
)

// TODO: share the structs in projects
type AppMetaData struct {
	Name        string   `yaml:"name" json:"name"`
	Icon        string   `yaml:"icon" json:"icon"`
	Description string   `yaml:"description" json:"description"`
	AppID       string   `yaml:"appid" json:"appid"`
	Title       string   `yaml:"title" json:"title"`
	Version     string   `yaml:"version" json:"version"`
	Categories  []string `yaml:"categories" json:"categories"`
	Rating      float32  `yaml:"rating" json:"rating"`
	Target      string   `yaml:"target" json:"target"`
	Type        string   `yaml:"type" json:"type"`
}

type AppConfiguration struct {
	ConfigVersion string                 `yaml:"olaresManifest.version" json:"olaresManifest.version"`
	Metadata      AppMetaData            `yaml:"metadata" json:"metadata"`
	Entrances     []v1alpha1.Entrance    `yaml:"entrances" json:"entrances"`
	Ports         []v1alpha1.ServicePort `yaml:"ports" json:"ports"`
	TailScaleACLs []v1alpha1.ACL         `yaml:"tailscaleAcls" json:"tailscaleAcls"`
	Spec          AppSpec                `yaml:"spec" json:"spec"`
	Permission    Permission             `yaml:"permission" json:"permission" description:"app permission request"`
	Middleware    *tapr.Middleware       `yaml:"middleware" json:"middleware" description:"app middleware request"`
	Options       Options                `yaml:"options" json:"options" description:"app options"`
}

type AppSpec struct {
	Namespace          string        `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	OnlyAdmin          bool          `yaml:"onlyAdmin,omitempty" json:"onlyAdmin,omitempty"`
	VersionName        string        `yaml:"versionName" json:"versionName"`
	FullDescription    string        `yaml:"fullDescription" json:"fullDescription"`
	UpgradeDescription string        `yaml:"upgradeDescription" json:"upgradeDescription"`
	PromoteImage       []string      `yaml:"promoteImage" json:"promoteImage"`
	PromoteVideo       string        `yaml:"promoteVideo" json:"promoteVideo"`
	SubCategory        string        `yaml:"subCategory" json:"subCategory"`
	Developer          string        `yaml:"developer" json:"developer"`
	RequiredMemory     string        `yaml:"requiredMemory" json:"requiredMemory"`
	RequiredDisk       string        `yaml:"requiredDisk" json:"requiredDisk"`
	SupportClient      SupportClient `yaml:"supportClient" json:"supportClient"`
	RequiredGPU        string        `yaml:"requiredGpu" json:"requiredGpu"`
	RequiredCPU        string        `yaml:"requiredCpu" json:"requiredCpu"`
	RunAsUser          bool          `yaml:"runAsUser" json:"runAsUser"`
}

type SupportClient struct {
	Edge    string `yaml:"edge" json:"edge"`
	Android string `yaml:"android" json:"android"`
	Ios     string `yaml:"ios" json:"ios"`
	Windows string `yaml:"windows" json:"windows"`
	Mac     string `yaml:"mac" json:"mac"`
	Linux   string `yaml:"linux" json:"linux"`
}

type Permission struct {
	AppData  bool         `yaml:"appData" json:"appData"  description:"app data permission for writing"`
	AppCache bool         `yaml:"appCache" json:"appCache"`
	UserData []string     `yaml:"userData" json:"userData"`
	SysData  []SysDataCfg `yaml:"sysData" json:"sysData"  description:"system shared data permission for accessing"`
}

type SysDataCfg struct {
	AppName   string   `yaml:"appName" json:"appName"`
	Svc       string   `yaml:"svc,omitempty" json:"svc,omitempty"`
	Namespace string   `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Port      string   `yaml:"port" json:"port"`
	Group     string   `yaml:"group" json:"group"`
	DataType  string   `yaml:"dataType" json:"dataType"`
	Version   string   `yaml:"version" json:"version"`
	Ops       []string `yaml:"ops" json:"ops"`
}

type Policy struct {
	EntranceName string `yaml:"entranceName" json:"entranceName"`
	Description  string `yaml:"description" json:"description" description:"the description of the policy"`
	URIRegex     string `yaml:"uriRegex" json:"uriRegex" description:"uri regular expression"`
	Level        string `yaml:"level" json:"level"`
	OneTime      bool   `yaml:"oneTime" json:"oneTime"`
	Duration     string `yaml:"validDuration" json:"validDuration"`
}

type Analytics struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type Dependency struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
	// dependency type: system, application.
	Type string `yaml:"type" json:"type"`
}

type Options struct {
	MobileSupported bool                     `yaml:"mobileSupported" json:"mobileSupported"`
	Policies        []Policy                 `yaml:"policies" json:"policies"`
	Analytics       Analytics                `yaml:"analytics" json:"analytics"`
	ResetCookie     ResetCookie              `yaml:"resetCookie" json:"resetCookie"`
	Dependencies    []Dependency             `yaml:"dependencies" json:"dependencies"`
	AppScope        AppScope                 `yaml:"appScope" json:"appScope"`
	WsConfig        WsConfig                 `yaml:"websocket" json:"websocket"`
	Upload          Upload                   `yaml:"upload" json:"upload"`
	SyncProvider    []map[string]interface{} `yaml:"syncProvider" json:"syncProvider"`
	OIDC            OIDC                     `yaml:"oidc" json:"oidc"`
	ApiTimeout      *int64                   `yaml:"apiTimeout" json:"apiTimeout"`
}

type ResetCookie struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type AppScope struct {
	ClusterScoped bool     `yaml:"clusterScoped" json:"clusterScoped"`
	AppRef        []string `yaml:"appRef" json:"appRef"`
}

type WsConfig struct {
	Port int    `yaml:"port" json:"port"`
	URL  string `yaml:"url" json:"url"`
}

type Upload struct {
	FileType    []string `yaml:"fileType" json:"fileType"`
	Dest        string   `yaml:"dest" json:"dest"`
	LimitedSize int      `yaml:"limitedSize" json:"limitedSize"`
}

type OIDC struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	RedirectUri  string `yaml:"redirectUri" json:"redirectUri"`
	EntranceName string `yaml:"entranceName" json:"entranceName"`
}
