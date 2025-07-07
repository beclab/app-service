package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"

	"github.com/Masterminds/semver/v3"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// AppCfgFileName config file name for application.
	AppCfgFileName = "OlaresManifest.yaml"

	MinCfgFileVersion = ">= 0.7.2"
)

// GetAppConfig get app installation configuration from app store
func GetAppConfig(ctx context.Context, app, owner, cfgURL, repoURL, version, token, admin, marketSource string) (*appcfg.ApplicationConfig, string, error) {
	if repoURL == "" {
		return nil, "", fmt.Errorf("url info is empty, cfg [%s], repo [%s]", cfgURL, repoURL)
	}

	var (
		appcfg    *appcfg.ApplicationConfig
		chartPath string
		err       error
	)

	if cfgURL != "" {
		appcfg, chartPath, err = getAppConfigFromURL(ctx, app, cfgURL)
		if err != nil {
			return nil, "", err
		}
	} else {
		appcfg, chartPath, err = getAppConfigFromRepo(ctx, app, repoURL, version, token, owner, admin, marketSource)
		if err != nil {
			return nil, chartPath, err
		}
	}

	// set appcfg.Namespace to specified namespace by OlaresManifests.Spec
	var namespace string
	if appcfg.Namespace != "" {
		namespace, _ = utils.AppNamespace(app, owner, appcfg.Namespace)
	} else {
		namespace = fmt.Sprintf("%s-%s", app, owner)
	}

	appcfg.Namespace = namespace
	appcfg.OwnerName = owner
	appcfg.RepoURL = repoURL
	return appcfg, chartPath, nil
}

func getAppConfigFromConfigurationFile(app, chart, owner, admin string) (*appcfg.ApplicationConfig, string, error) {
	data, err := utils.RenderManifest(filepath.Join(chart, AppCfgFileName), owner, admin)
	if err != nil {
		return nil, chart, err
	}

	var cfg appcfg.AppConfiguration
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, chart, err
	}

	return toApplicationConfig(app, chart, &cfg)
}

func getAppConfigFromURL(ctx context.Context, app, url string) (*appcfg.ApplicationConfig, string, error) {
	client := resty.New().SetTimeout(2 * time.Second)
	resp, err := client.R().Get(url)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode() >= 400 {
		return nil, "", fmt.Errorf("app config url returns unexpected status code, %d", resp.StatusCode())
	}

	var cfg appcfg.AppConfiguration
	if err := yaml.Unmarshal(resp.Body(), &cfg); err != nil {
		return nil, "", err
	}

	return toApplicationConfig(app, app, &cfg)
}

func getAppConfigFromRepo(ctx context.Context, app, repoURL, version, token, owner, admin, marketSource string) (*appcfg.ApplicationConfig, string, error) {
	chartPath, err := apputils.GetIndexAndDownloadChart(ctx, app, repoURL, version, token, owner, marketSource)
	if err != nil {
		return nil, chartPath, err
	}
	return getAppConfigFromConfigurationFile(app, chartPath, owner, admin)
}

func toApplicationConfig(app, chart string, cfg *appcfg.AppConfiguration) (*appcfg.ApplicationConfig, string, error) {
	var permission []appcfg.AppPermission
	if cfg.Permission.AppData {
		permission = append(permission, appcfg.AppDataRW)
	}
	if cfg.Permission.AppCache {
		permission = append(permission, appcfg.AppCacheRW)
	}
	if len(cfg.Permission.UserData) > 0 {
		permission = append(permission, appcfg.UserDataRW)
	}

	if len(cfg.Permission.SysData) > 0 {
		var perm []appcfg.SysDataPermission
		for _, s := range cfg.Permission.SysData {
			perm = append(perm, appcfg.SysDataPermission{
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
		return nil, chart, err
	}

	disk, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredDisk))
	if err != nil {
		return nil, chart, err
	}

	cpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredCPU))
	if err != nil {
		return nil, chart, err
	}

	gpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredGPU))
	if err != nil {
		return nil, chart, err
	}

	// transform from Policy to AppPolicy
	var policies []appcfg.AppPolicy
	for _, p := range cfg.Options.Policies {
		d, _ := time.ParseDuration(p.Duration)
		policies = append(policies, appcfg.AppPolicy{
			EntranceName: p.EntranceName,
			URIRegex:     p.URIRegex,
			Level:        p.Level,
			OneTime:      p.OneTime,
			Duration:     d,
		})
	}

	// check dependencies version format
	for _, dep := range cfg.Options.Dependencies {
		if err = checkVersionFormat(dep.Version); err != nil {
			return nil, chart, err
		}
	}

	if cfg.Middleware != nil && cfg.Middleware.Redis != nil {
		if len(cfg.Middleware.Redis.Namespace) == 0 {
			return nil, chart, errors.New("middleware of Redis namespace can not be empty")
		}
	}
	var appid string
	if userspace.IsSysApp(app) {
		appid = app
	} else {
		appid = utils.Md5String(app)[:8]
	}

	return &appcfg.ApplicationConfig{
		AppID:          appid,
		CfgFileVersion: cfg.ConfigVersion,
		AppName:        app,
		Title:          cfg.Metadata.Title,
		Version:        cfg.Metadata.Version,
		Target:         cfg.Metadata.Target,
		ChartsName:     chart,
		Entrances:      cfg.Entrances,
		Ports:          cfg.Ports,
		TailScale:      cfg.TailScale,
		Icon:           cfg.Metadata.Icon,
		Permission:     permission,
		Requirement: appcfg.AppRequirement{
			Memory: mem,
			CPU:    cpu,
			Disk:   disk,
			GPU:    gpu,
		},
		Policies:             policies,
		Middleware:           cfg.Middleware,
		ResetCookieEnabled:   cfg.Options.ResetCookie.Enabled,
		Dependencies:         cfg.Options.Dependencies,
		Conflicts:            cfg.Options.Conflicts,
		AppScope:             cfg.Options.AppScope,
		WsConfig:             cfg.Options.WsConfig,
		Upload:               cfg.Options.Upload,
		OnlyAdmin:            cfg.Spec.OnlyAdmin,
		Namespace:            cfg.Spec.Namespace,
		MobileSupported:      cfg.Options.MobileSupported,
		OIDC:                 cfg.Options.OIDC,
		ApiTimeout:           cfg.Options.ApiTimeout,
		RunAsUser:            cfg.Spec.RunAsUser,
		AllowedOutboundPorts: cfg.Options.AllowedOutboundPorts,
	}, chart, nil
}

func checkVersionFormat(constraint string) error {
	_, err := semver.NewConstraint(constraint)
	if err != nil {
		return err
	}
	return nil
}
