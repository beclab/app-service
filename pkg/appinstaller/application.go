package appinstaller

import (
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

	"k8s.io/apimachinery/pkg/api/resource"
)

type AppPermission interface{}

type AppDataPermission string
type AppCachePermission string
type UserDataPermission string

type Middleware interface{}

type SysDataPermission struct {
	Group    string   `yaml:"group" json:"group"`
	DataType string   `yaml:"dataType" json:"dataType"`
	Version  string   `yaml:"version" json:"version"`
	Ops      []string `yaml:"ops" json:"ops"`
}

type AppRequirement struct {
	Memory *resource.Quantity
	Disk   *resource.Quantity
	GPU    *resource.Quantity
	CPU    *resource.Quantity
}

type AppPolicy struct {
	EntranceName string        `yaml:"entranceName" json:"entranceName"`
	URIRegex     string        `yaml:"uriRegex" json:"uriRegex" description:"uri regular expression"`
	Level        string        `yaml:"level" json:"level"`
	OneTime      bool          `yaml:"oneTime" json:"oneTime"`
	Duration     time.Duration `yaml:"validDuration" json:"validDuration"`
}

const (
	AppDataRW  AppDataPermission  = "appdata-perm"
	AppCacheRW AppCachePermission = "appcache-perm"
	UserDataRW UserDataPermission = "userdata-perm"
)

type ApplicationConfig struct {
	AppID              string
	Namespace          string
	ChartsName         string
	RepoURL            string
	Title              string
	Version            string
	Target             string
	AppName            string // name of application displayed on shortcut
	OwnerName          string // name of owner who installed application
	Entrances          []v1alpha1.Entrance
	Icon               string          // base64 icon data
	Permission         []AppPermission // app permission requests
	Requirement        AppRequirement
	Policies           []AppPolicy
	Middleware         Middleware
	AnalyticsEnabled   bool
	ResetCookieEnabled bool
	Dependencies       []Dependency
	AppScope           AppScope
	WsConfig           WsConfig
	Upload             Upload
	OnlyAdmin          bool
	MobileSupported    bool
	OIDC               OIDC
}
