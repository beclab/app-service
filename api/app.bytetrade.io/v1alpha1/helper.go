package v1alpha1

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"k8s.io/klog/v2"
)

func (a *Application) IsClusterScoped() bool {
	if a.Spec.Settings == nil {
		return false
	}
	if v, ok := a.Spec.Settings["clusterScoped"]; ok && v == "true" {
		return true
	}
	return false
}

func (a *ApplicationManager) GetAppConfig(appConfig any) (err error) {
	err = json.Unmarshal([]byte(a.Spec.Config), &appConfig)
	if err != nil {
		klog.Errorf("unmarshal to appConfig failed %v", err)
		return err
	}

	return
}

func (a *ApplicationManager) SetAppConfig(appConfig any) error {
	configBytes, err := json.Marshal(appConfig)
	if err != nil {
		klog.Errorf("marshal appConfig failed %v", err)
		return err
	}

	a.Spec.Config = string(configBytes)

	return nil
}

type AppName string

func (s AppName) GetAppID() string {
	if s.IsSysApp() {
		return string(s)
	}
	hash := md5.Sum([]byte(s))
	hashString := hex.EncodeToString(hash[:])
	return hashString[:8]
}

func (s AppName) String() string {
	return string(s)
}

func (s AppName) IsSysApp() bool {
	return userspace.IsSysApp(string(s))
}

func (s AppName) IsGeneratedApp() bool {
	return userspace.IsGeneratedApp(string(s))
}

func (app *Application) GenEntranceURL(ctx context.Context) ([]Entrance, error) {
	zone, err := kubesphere.GetUserZone(ctx, app.Spec.Owner)
	if err != nil {
		klog.Errorf("failed to get user zone: %v", err)
		return nil, err
	}

	if len(zone) > 0 {
		appid := AppName(app.Spec.Name).GetAppID()
		if len(app.Spec.Entrances) == 1 {
			app.Spec.Entrances[0].URL = fmt.Sprintf("%s.%s", appid, zone)
		}
		for i := range app.Spec.Entrances {
			app.Spec.Entrances[i].URL = fmt.Sprintf("%s%d.%s", appid, i, zone)
		}
	}
	return app.Spec.Entrances, nil
}
