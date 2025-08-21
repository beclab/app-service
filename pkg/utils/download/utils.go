package download

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"bytetrade.io/web3os/app-service/pkg/utils/files"

	"github.com/go-resty/resty/v2"
	"github.com/hashicorp/go-getter"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// GetIndexAndDownloadChart download a chart and returns download chart path.
func GetIndexAndDownloadChart(ctx context.Context, app, repoURL, version, token, owner, marketSource string) (string, error) {
	terminusNonce, err := utils.GenTerminusNonce()
	if err != nil {
		return "", err
	}
	client := resty.New().SetTimeout(10*time.Second).
		SetHeader(constants.AuthorizationTokenKey, token).
		SetAuthToken(token).
		SetHeader("Terminus-Nonce", terminusNonce).
		SetHeader(constants.MarketUser, owner).
		SetHeader(constants.MarketSource, marketSource)
	indexFileURL := repoURL
	if repoURL[len(repoURL)-1] != '/' {
		indexFileURL += "/"
	}
	klog.Infof("GetIndexAndDownloadChart: user: %v, source: %v", owner, marketSource)
	indexFileURL += "index.yaml"
	resp, err := client.R().Get(indexFileURL)
	if err != nil {
		return "", err
	}

	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("get app config from repo returns unexpected status code, %d", resp.StatusCode())
	}

	index, err := loadIndex(resp.Body())
	if err != nil {
		klog.Errorf("Failed to load chart index err=%v", err)
		return "", err
	}

	klog.Infof("Success to find app chart from index app=%s version=%s", app, version)
	// get specified version chart, if version is empty return the chart with the latest stable version
	chartVersion, err := index.Get(app, version)

	if err != nil {
		klog.Errorf("Failed to get chart version err=%v", err)
		return "", fmt.Errorf("app [%s-%s] not found in repo", app, version)
	}

	chartURL, err := repo.ResolveReferenceURL(repoURL, chartVersion.URLs[0])
	if err != nil {
		return "", err
	}

	url, err := url.Parse(chartURL)
	if err != nil {
		return "", err
	}

	// assume the chart path is app name
	chartPath := appcfg.ChartsPath + "/" + app
	if files.IsExist(chartPath) {
		if err := files.RemoveAll(chartPath); err != nil {
			return "", err
		}
	}
	_, err = downloadAndUnpack(ctx, url, token, terminusNonce, owner, marketSource)
	if err != nil {
		return "", err
	}
	return chartPath, nil
}

func downloadAndUnpack(ctx context.Context, tgz *url.URL, token, terminusNonce, owner, marketSource string) (string, error) {
	dst := appcfg.ChartsPath
	g := new(getter.HttpGetter)
	g.Header = make(http.Header)
	g.Header.Set(constants.AuthorizationTokenKey, token)
	g.Header.Set("Terminus-Nonce", terminusNonce)
	g.Header.Set(constants.MarketUser, owner)
	g.Header.Set(constants.MarketSource, marketSource)
	downloader := &getter.Client{
		Ctx:       ctx,
		Dst:       dst,
		Src:       tgz.String(),
		Mode:      getter.ClientModeDir,
		Detectors: getter.Detectors,
		Getters: map[string]getter.Getter{
			"http": g,
			"file": new(getter.FileGetter),
		},
	}

	//download the files
	if err := downloader.Get(); err != nil {
		klog.Errorf("Failed to get path=%s err=%v", downloader.Src, err)
		return "", err
	}

	return dst, nil
}

func loadIndex(data []byte) (*repo.IndexFile, error) {
	i := &repo.IndexFile{}

	if len(data) == 0 {
		return i, repo.ErrEmptyIndexYaml
	}

	if err := yaml.UnmarshalStrict(data, i); err != nil {
		return i, err
	}

	for name, cvs := range i.Entries {
		for idx := len(cvs) - 1; idx >= 0; idx-- {
			if cvs[idx].APIVersion == "" {
				cvs[idx].APIVersion = chart.APIVersionV1
			}
			if err := cvs[idx].Validate(); err != nil {
				klog.Infof("Skipping loading invalid entry for chart name=%q version=%q err=%v", name, cvs[idx].Version, err)
				cvs = append(cvs[:idx], cvs[idx+1:]...)
			}
		}
	}
	i.SortEntries()
	if i.APIVersion == "" {
		return i, repo.ErrNoAPIVersion
	}
	return i, nil
}
