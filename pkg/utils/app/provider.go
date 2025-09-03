package app

import (
	"context"
	"errors"
	"fmt"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ProviderPermissionHelper appcfg.ProviderPermission
type ProviderPermissionsConvertor []appcfg.ProviderPermission
type ProviderHelper struct {
	appcfg.Provider
	appCfg *appcfg.ApplicationConfig
}

func (c ProviderPermissionsConvertor) ToPermissionCfg(ctx context.Context, owner string, marksetSrouce string) (cfg []appcfg.PermissionCfg, err error) {
	if len(c) == 0 {
		return nil, nil
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("Failed to get kube config: %v", err)
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Errorf("Failed to create kube client: %v", err)
		return nil, err
	}

	token, err := utils.GetUserServiceAccountToken(ctx, kubeClient, owner)
	if err != nil {
		klog.Errorf("Failed to get service account token: %v", err)
		return nil, err
	}

	appCfgMap := make(map[string]*appcfg.ApplicationConfig)

	const defaultMarketSource = "market.olares"
	var marketSources []string
	if marksetSrouce != "" && marksetSrouce != defaultMarketSource {
		marketSources = append(marketSources, marksetSrouce)
	}
	marketSources = append(marketSources, defaultMarketSource)
	klog.Info("try to find provider from market source, ", marketSources)
	for _, p := range c {
		appCfg, ok := appCfgMap[p.AppName]
		if !ok {
			for _, m := range marketSources {
				o := ConfigOptions{
					App:          p.AppName,
					RepoURL:      constants.CHART_REPO_URL,
					Owner:        owner,
					Version:      "",
					Token:        token,
					Admin:        owner,
					MarketSource: m,
					IsAdmin:      false,
				}
				appCfg, _, err = GetAppConfig(ctx, &o)
				if err != nil {
					klog.Errorf("Failed to get app config for %s: %v", p.AppName, err)
					if errors.Is(err, ErrAppNotFoundInChartRepo) {
						continue
					}
					return nil, err
				}

				if appCfg != nil {
					break
				}
			}

			if appCfg == nil {
				continue
			}

			appCfgMap[p.AppName] = appCfg
		}

		pc, err := ProviderPermissionHelper(p).GetPermissionCfg(ctx, appCfg)
		if err != nil {
			klog.Errorf("Failed to get permission config for %s: %v", p.AppName, err)
			if errors.Is(err, ErrProviderNotFound) {
				continue
			}
			return nil, err
		}
		cfg = append(cfg, *pc)
	} // end of for loop

	return cfg, nil
}

func (h ProviderPermissionHelper) GetPermissionCfg(ctx context.Context, appCfg *appcfg.ApplicationConfig) (*appcfg.PermissionCfg, error) {
	for _, p := range appCfg.Provider {
		if p.Name == h.ProviderName {
			entrance, err := (&ProviderHelper{p, appCfg}).GetEntrance(ctx)
			if err != nil {
				klog.Errorf("Failed to get entrance for provider %s: %v", h.ProviderName, err)
				return nil, err
			}

			return &appcfg.PermissionCfg{
				ProviderPermission: (*appcfg.ProviderPermission)(&h),
				Port:               int(entrance.Port),
				Svc:                entrance.Host,
				Domain:             entrance.URL,
				Paths:              p.Paths,
			}, nil

		}
	} // end of providers loop

	klog.Errorf("provider %s not found in app %s", h.ProviderName, appCfg.AppName)
	return nil, ErrProviderNotFound
}

func (p *ProviderHelper) GetEntrance(ctx context.Context) (*v1alpha1.Entrance, error) {
	if p.appCfg == nil {
		return nil, fmt.Errorf("application config is not set for provider %s", p.Name)
	}

	entrances, err := p.appCfg.GetEntrances(ctx)
	if err != nil {
		klog.Errorf("failed to get entrance map for app %s: %v", p.appCfg.AppName, err)
		return nil, err
	}

	entrance, ok := entrances[p.Entrance]
	if !ok {
		return nil, fmt.Errorf("entrance %s not found for provider %s", p.Entrance, p.Name)
	}

	return &entrance, nil
}
