package upgrade

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"k8s.io/klog/v2"
)

type DownloadRelease struct {
	jobCtx    context.Context
	jobCancel context.CancelFunc
	devMode   bool
}

var CDN = "https://dc3p1870nn3cj.cloudfront.net"

func (d *DownloadRelease) Run(ctx *PipelineContext) error {
	d.jobCtx, d.jobCancel = context.WithCancel(ctx.InstallCtx)
	installed, err := GetTerminusVersion(d.jobCtx, ctx.K8sClient)
	if err != nil {
		return err
	}

	switch installed.Spec.ReleaseServer.ServerType {
	case GITHUB:
		github := NewGithubRelease(
			ctx.httpClient,
			installed.Spec.ReleaseServer.Github.Owner,
			installed.Spec.ReleaseServer.Github.Repo,
		)

		klog.Info("start to download release package, ", ctx.ReleaseVersion)
		_, installTgz, _, err := github.getReleaseBundle(d.jobCtx, ctx.ReleaseVersion)
		if err != nil {
			return err
		}

		ctx.InstallPackgePath, err = github.downloadAndUnpack(d.jobCtx, installTgz)
		if err != nil {
			klog.Error(err)
			cdn := os.Getenv("DOWNLOAD_CDN_URL")
			if cdn != "" {
				CDN = cdn
			}
			urlStrTokens := strings.Split(installTgz.String(), "/")
			cdnUrlStr := strings.Join([]string{CDN, urlStrTokens[len(urlStrTokens)-1]}, "/")
			cdnUrl, err := url.Parse(cdnUrlStr)
			if err != nil {
				klog.Error("get cdn url error, ", err)
				return err
			}

			klog.Warning("failed to download release package, ", ctx.ReleaseVersion, ", try to download from cdn, ", cdnUrlStr)
			ctx.InstallPackgePath, err = github.downloadAndUnpack(d.jobCtx, cdnUrl)
			if err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("unsupported release server type: %s", installed.Spec.ReleaseServer.ServerType)
	}

	return nil
}

func (d *DownloadRelease) Cancel() {
	d.jobCancel()
}

type RunUpgradeCmd struct {
	jobCtx    context.Context
	jobCancel context.CancelFunc
}

func (r *RunUpgradeCmd) Run(ctx *PipelineContext) error {
	r.jobCtx, r.jobCancel = context.WithCancel(ctx.InstallCtx)

	script := ctx.InstallPackgePath + "/upgrade_cmd.sh"
	cmd := exec.CommandContext(r.jobCtx, "bash", script)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(), "BASE_DIR="+ctx.InstallPackgePath)

	return cmd.Run()
}

func (r *RunUpgradeCmd) Cancel() {
	r.jobCancel()
}
