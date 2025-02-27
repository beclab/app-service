package appinstaller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	ChartsPath = "./charts"
)

func AppChartPath(app string) string {
	return ChartsPath + "/" + app
}

// GetAppInstallationConfig get app installation configuration from app store
func GetAppInstallationConfig(app, owner string) (*ApplicationConfig, error) {
	chart := AppChartPath(app)
	appcfg, err := getAppConfigFromConfigurationFile(app, chart)
	if err != nil {
		return nil, err
	}

	// TODO: app installation namespace
	var namespace string
	if appcfg.Namespace != "" {
		namespace, _ = utils.AppNamespace(app, owner, appcfg.Namespace)
	} else {
		namespace = fmt.Sprintf("%s-%s", app, owner)
	}

	appcfg.Namespace = namespace
	appcfg.OwnerName = owner

	return appcfg, nil
}

func getAppConfigFromConfigurationFile(app, chart string) (*ApplicationConfig, error) {
	f, err := os.Open(filepath.Join(chart, "OlaresManifest.yaml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var cfg AppConfiguration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	var permission []AppPermission
	if cfg.Permission.AppData {
		permission = append(permission, AppDataRW)
	}
	if cfg.Permission.AppCache {
		permission = append(permission, AppCacheRW)
	}
	if len(cfg.Permission.UserData) > 0 {
		permission = append(permission, UserDataRW)
	}

	if len(cfg.Permission.SysData) > 0 {
		var perm []SysDataPermission
		for _, s := range cfg.Permission.SysData {
			perm = append(perm, SysDataPermission{
				AppName:   s.AppName,
				Svc:       s.Svc,
				Namespace: s.Namespace,
				Port:      s.Port,
				Group:     s.Group,
				DataType:  s.DataType,
				Version:   s.Version,
				Ops:       s.Ops,
			})
		}
		permission = append(permission, perm)
	}

	valuePtr := func(v resource.Quantity, err error) (*resource.Quantity, error) {
		if errors.Is(err, resource.ErrFormatWrong) {
			return nil, nil
		}

		return &v, nil
	}

	mem, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredMemory))
	if err != nil {
		return nil, err
	}

	disk, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredDisk))
	if err != nil {
		return nil, err
	}

	cpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredCPU))
	if err != nil {
		return nil, err
	}
	gpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredGPU))
	if err != nil {
		return nil, err
	}

	var polices []AppPolicy
	if len(cfg.Options.Policies) > 0 {
		for _, p := range cfg.Options.Policies {
			duration, err := time.ParseDuration(p.Duration)
			if err != nil {
				klog.Errorf("Failed to parse app cfg options policy duration err=%v", err)
			}
			polices = append(polices, AppPolicy{
				EntranceName: p.EntranceName,
				URIRegex:     p.URIRegex,
				Level:        p.Level,
				OneTime:      p.OneTime,
				Duration:     duration,
			})
		}
	}

	return &ApplicationConfig{
		AppID:          cfg.Metadata.AppID,
		CfgFileVersion: cfg.ConfigVersion,
		AppName:        app,
		Title:          cfg.Metadata.Title,
		Version:        cfg.Metadata.Version,
		Target:         cfg.Metadata.Target,
		ChartsName:     chart,
		Entrances:      cfg.Entrances,
		Ports:          cfg.Ports,
		TailScaleACLs:  cfg.TailScaleACLs,
		Icon:           cfg.Metadata.Icon,
		Permission:     permission,
		Requirement: AppRequirement{
			Memory: mem,
			CPU:    cpu,
			Disk:   disk,
			GPU:    gpu,
		},
		Policies:             polices,
		AnalyticsEnabled:     cfg.Options.Analytics.Enabled,
		ResetCookieEnabled:   cfg.Options.ResetCookie.Enabled,
		Dependencies:         cfg.Options.Dependencies,
		AppScope:             cfg.Options.AppScope,
		OnlyAdmin:            cfg.Spec.OnlyAdmin,
		Namespace:            cfg.Spec.Namespace,
		MobileSupported:      cfg.Options.MobileSupported,
		OIDC:                 cfg.Options.OIDC,
		ApiTimeout:           cfg.Options.ApiTimeout,
		AllowedOutboundPorts: cfg.Options.AllowedOutboundPorts,
	}, nil
}

// GetAppConfigFromCRD et app uninstallation config from crd
func GetAppConfigFromCRD(app, owner string,
	client *clientset.ClientSet, req *restful.Request) (*ApplicationConfig, error) {
	// run with request context for incoming client
	applist, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get by application's owner and name
	for _, a := range applist.Items {
		if a.Spec.Owner == owner && a.Spec.Name == app {
			// TODO: other configs
			return &ApplicationConfig{
				AppName:   app,
				Namespace: a.Spec.Namespace,
				//ChartsName: "charts/apps",
				OwnerName: owner,
			}, nil
		}
	}

	return nil, api.ErrResourceNotFound
}

func ToEntrances(s string) (entrances []v1alpha1.Entrance, err error) {
	err = json.Unmarshal([]byte(s), &entrances)
	if err != nil {
		return entrances, err
	}

	return entrances, nil
}

func ToEntrancesLabel(entrances []v1alpha1.Entrance) string {
	serviceLabel, _ := json.Marshal(entrances)
	return string(serviceLabel)
}

func ToAppTCPUDPPorts(ports []v1alpha1.ServicePort) string {
	ret := make([]v1alpha1.ServicePort, 0)
	for _, port := range ports {
		protos := []string{port.Protocol}
		if port.Protocol == "" {
			protos = []string{"tcp", "udp"}
		}
		for _, proto := range protos {
			ret = append(ret, v1alpha1.ServicePort{
				Name:              port.Name,
				Host:              port.Host,
				Port:              port.Port,
				ExposePort:        port.ExposePort,
				Protocol:          proto,
				AddToTailscaleAcl: port.AddToTailscaleAcl,
			})
		}
	}
	portsLabel, _ := json.Marshal(ret)
	return string(portsLabel)
}

func ToTailScaleACL(acls []v1alpha1.ACL) string {
	aclLabel, _ := json.Marshal(acls)
	return string(aclLabel)
}
