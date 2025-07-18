package v1alpha1

import (
	"encoding/json"

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
