package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v50/github"
	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/go-version"
	gover "github.com/hashicorp/go-version"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
)

type GithubRelease struct {
	client *github.Client
	owner  string
	repo   string
}

func NewGithubRelease(httpClient *http.Client, owner, repo string) *GithubRelease {
	return &GithubRelease{
		client: github.NewClient(httpClient),
		owner:  owner,
		repo:   repo,
	}
}

func (g *GithubRelease) getRelease(ctx context.Context, tag string) (*github.RepositoryRelease, error) {
	release, resp, err := g.client.Repositories.GetReleaseByTag(ctx, g.owner, g.repo, tag)

	if err != nil {
		klog.Errorf("Failed to get version from github err=%v", err)

		return nil, err
	}

	if resp.Response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github repo release api return not 200, got %d", resp.Response.StatusCode)
	}

	return release, nil
}

func (g *GithubRelease) getLatestRelease(ctx context.Context, devmode bool) (*github.RepositoryRelease, error) {
	release, resp, err := g.client.Repositories.GetLatestRelease(ctx, g.owner, g.repo)
	if err != nil {
		klog.Errorf("Failed to get version from github err=%v", err)

		return nil, err
	}

	if resp.Response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github repo release api return not 200, got %d", resp.Response.StatusCode)
	}

	if devmode {
		return release, nil
	}
	if !strings.Contains(strings.ToLower(*release.TagName), "rc") {
		return release, nil
	}

	// desc
	releaseList, resp, err := g.client.Repositories.ListReleases(ctx, g.owner, g.repo, &github.ListOptions{PerPage: 50})
	if err != nil {
		klog.Errorf("Failed to list version from github err=%v", err)

		return nil, err
	}

	if resp.Response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github repo release api return not 200, got %d", resp.Response.StatusCode)
	}

	for _, r := range releaseList {
		if !*r.Draft && !strings.Contains(strings.ToLower(*r.TagName), "rc") {
			return r, nil
		}
	}

	return nil, errors.New("release not found")

}

func (g *GithubRelease) getLatestReleaseVersion(ctx context.Context, devmode bool) (*gover.Version, error) {
	release, err := g.getLatestRelease(ctx, devmode)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}

	return gover.NewVersion(*release.TagName)
}

func (g *GithubRelease) getBundle(release *github.RepositoryRelease) (upgradeTar *url.URL, fullTar *url.URL, versionHint *url.URL, err error) {
	for _, a := range release.Assets {
		switch {
		case g.isFullInstallPackage(a.GetName()):
			u := a.GetBrowserDownloadURL()
			if fullTar, err = url.ParseRequestURI(u); err != nil {
				klog.Errorf("Failed to parse raw url=%s err=%v", u, err)

				return nil, nil, nil, err
			}
		case g.isUpgradePackage(a.GetName()):
			u := a.GetBrowserDownloadURL()
			if upgradeTar, err = url.ParseRequestURI(u); err != nil {
				klog.Errorf("Failed to parse raw url=%s err=%v", u, err)

				return nil, nil, nil, err
			}
		case g.isVersionHint(a.GetName()):
			u := a.GetBrowserDownloadURL()
			if versionHint, err = url.ParseRequestURI(u); err != nil {
				klog.Errorf("Failed to parse raw url=%s err=%v", u, err)
				return nil, nil, nil, err
			}

		}
	}

	return upgradeTar, fullTar, versionHint, nil
}

func (g *GithubRelease) getReleaseBundle(ctx context.Context, releaseVersion *version.Version) (upgradeTar *url.URL, fullTar *url.URL, versionHint *url.URL, err error) {
	release, err := g.getRelease(ctx, releaseVersion.Original())
	if err != nil {
		klog.Errorf("Failed to get release bundle from github err=%v", err)

		return nil, nil, nil, err
	}

	return g.getBundle(release)
}

func (g *GithubRelease) getLatestReleaseBundle(ctx context.Context, devmode bool) (upgradeTar *url.URL, fullTar *url.URL, versionHint *url.URL, err error) {
	release, err := g.getLatestRelease(ctx, devmode)
	if err != nil {
		klog.Errorf("Failed to get release bundle from github err=%v", err)

		return nil, nil, nil, err
	}

	return g.getBundle(release)
}

func (g *GithubRelease) downloadAndUnpack(ctx context.Context, tgz *url.URL) (string, error) {
	dst, err := ioutil.TempDir("", "go-getter")
	if err != nil {
		klog.Errorf("Failed to get temp path err=%v", err)
		return "", err
	}

	downloader := &getter.Client{
		Ctx:       ctx,
		Dst:       dst,
		Src:       tgz.String(),
		Mode:      getter.ClientModeDir,
		Detectors: getter.Detectors,
		Getters:   getter.Getters,
	}

	//download the files
	if err := downloader.Get(); err != nil {
		klog.Errorf("Failed to get path=%s err=%v", downloader.Src, err)
		return "", err
	}

	return dst, nil
}

func (g *GithubRelease) readVersionHint(ctx context.Context, file *url.URL) (*VersionHint, error) {
	dst, err := ioutil.TempFile("", "version-hint")
	if err != nil {
		klog.Errorf("Failed to  get temp file err=%v", err)
		return nil, err
	}

	downloader := &getter.Client{
		Ctx:       ctx,
		Dst:       dst.Name(),
		Src:       file.String(),
		Mode:      getter.ClientModeFile,
		Detectors: getter.Detectors,
		Getters:   getter.Getters,
	}

	//download the files
	if err := downloader.Get(); err != nil {
		klog.Errorf("Failed to get file=%s err=%v", downloader.Src, err)
		return nil, err
	}

	data, err := ioutil.ReadAll(dst)
	if err != nil {
		klog.Errorf("Failed to read temp file err=%v", err)
		return nil, err
	}

	var hint VersionHint
	err = yaml.Unmarshal(data, &hint)
	if err != nil {
		klog.Errorf("Unable to unmarshal version hint file err=%v", err)
		return nil, err
	}

	return &hint, nil
}

func (g *GithubRelease) isFullInstallPackage(assetName string) bool {
	return strings.HasPrefix(assetName, "install-wizard-") && strings.HasSuffix(assetName, tgzPackageSuffix)
}

func (g *GithubRelease) isUpgradePackage(assetName string) bool {
	return strings.HasPrefix(assetName, "upgrade-") && strings.HasSuffix(assetName, tgzPackageSuffix)
}

func (g *GithubRelease) isVersionHint(assetName string) bool {
	return assetName == "version.hint"
}
