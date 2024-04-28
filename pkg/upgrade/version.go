package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"github.com/go-resty/resty/v2"
	gover "github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	httpClient = http.Client{
		Timeout: 2 * time.Second,
	}
)

type Upgrade struct {
	MinVersion string `yaml:"minVersion"`
}

type VersionHint struct {
	Upgrade Upgrade `yaml:"upgrade"`
}

type ReleaseHub interface {
	getLatestReleaseVersion(ctx context.Context) (*gover.Version, error)
}

func GetTerminusVersion(ctx context.Context, client client.Client) (*sysv1alpha1.Terminus, error) {
	key := types.NamespacedName{Name: "terminus"}
	//client, err := utils.GetClient()
	var terminus sysv1alpha1.Terminus
	err := client.Get(ctx, key, &terminus)
	if err != nil {
		return nil, err
	}

	return &terminus, nil
}

func GetTerminusReleaseVersion(ctx context.Context, terminus *sysv1alpha1.Terminus, devMode bool) (*gover.Version, *VersionHint, error) {
	switch terminus.Spec.ReleaseServer.ServerType {
	case GITHUB:
		type versionResp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    map[string]string
		}

		client := resty.New().SetTimeout(120 * time.Second).SetDebug(true)
		resp, err := client.R().
			SetFormData(map[string]string{
				"versions": terminus.Spec.Version,
				"devMode":  strconv.FormatBool(devMode),
			}).
			SetResult(&versionResp{}).
			Post("https://cloud-api.bttcdn.com/v1/resource/lastVersions")

		if err != nil {
			klog.Errorf("Failed to send request to terminus cloud err=%v", err)
			return nil, nil, err
		}

		if resp.StatusCode() != http.StatusOK {
			klog.Errorf("Failed to fetch upgradable version from terminus cloud response status=%s body=%s", resp.Status(), resp.Body())
			return nil, nil, errors.New(resp.Status())
		}

		version := resp.Result().(*versionResp)
		if version.Code != http.StatusOK {
			klog.Errorf("Terminus cloud response: code=%d message=%s", version.Code, version.Message)
			return nil, nil, errors.New(version.Message)
		}

		v, err := gover.NewSemver(version.Data["tag"])
		if err != nil {
			klog.Errorf("Terminus cloud response version is invalid err=%v tag=%s", err, version.Data["tag"])
			return nil, nil, err
		}

		hint := &VersionHint{
			Upgrade: Upgrade{
				MinVersion: version.Data["min"],
			},
		}

		return v, hint, nil
	default:
		return nil, nil, fmt.Errorf("unsupported release server type: %s", terminus.Spec.ReleaseServer.ServerType)
	}
}
