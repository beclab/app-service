package upgrade

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type DownloadRelease struct {
	jobCtx    context.Context
	jobCancel context.CancelFunc
	devMode   bool
}

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

		_, installTgz, _, err := github.getLatestReleaseBundle(d.jobCtx, d.devMode)
		if err != nil {
			return err
		}

		ctx.InstallPackgePath, err = github.downloadAndUnpack(d.jobCtx, installTgz)
		if err != nil {
			return err
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
