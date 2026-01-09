package appinstaller

import (
	"crypto/tls"
	"encoding/json"
	"github.com/go-resty/resty/v2"
	"k8s.io/klog/v2"
)

const (
	ShareModeExclusive   = "0"
	ShareModeMemSlicing  = "1"
	ShareModeTimeSlicing = "2"
)

type GPUDetail struct {
	NodeName  string       `json:"nodeName"`
	ID        string       `json:"id,omitempty"`
	Type      string       `json:"type,omitempty"`
	Mode      string       `json:"mode,omitempty"`
	ShareMode string       `json:"sharemode,omitempty"`
	Apps      []GPUAppInfo `json:"apps"`
}

type GPUAppInfo struct {
	AppName string `json:"appName"`
	Memory  *int64 `json:"memory,omitempty"`
}

func getGpuDetails() ([]GPUDetail, error) {
	url := "https://hami-scheduler.kube-system:443/gpus"
	client := resty.New().SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	resp, err := client.R().Get(url)
	if err != nil {
		klog.Errorf("failed to send request to %s %v", url, err)
		return nil, err
	}
	var gpuDetails []GPUDetail
	err = json.Unmarshal(resp.Body(), &gpuDetails)
	if err != nil {
		return nil, err
	}
	return gpuDetails, nil
}
