package upgrade

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	gover "github.com/hashicorp/go-version"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Upgrader struct {
	running *Pipeline
	lock    sync.Mutex
	DevMode bool
}

func NewUpgrader() *Upgrader {
	return &Upgrader{}
}

func (u *Upgrader) IsNewVersionValid(ctx context.Context, client client.Client, devMode bool) (isNew bool, currentVersion, releaseVersion *gover.Version, err error) {
	installed, err := GetTerminusVersion(ctx, client)
	if err != nil {
		return false, nil, nil, err
	}

	currentVersion, err = gover.NewVersion(installed.Spec.Version)
	if err != nil {
		klog.Errorf("Invalid terminus version=%s err=%v", installed.Spec.Version, err)
		return false, nil, nil, err
	}

	releaseVersion, hint, err := GetTerminusReleaseVersion(ctx, installed, devMode)
	if err != nil {
		return false, nil, nil, err
	}

	if releaseVersion == nil {
		return false, currentVersion, nil, nil
	}

	if hint != nil {
		minVersion, err := gover.NewVersion(hint.Upgrade.MinVersion)
		if err != nil {
			return false, nil, nil, err
		}

		if minVersion.GreaterThan(currentVersion) {
			klog.Warningf("Current version=%s is too old to upgrade version=%s", installed.Spec.Version, hint.Upgrade.MinVersion)
			return false, currentVersion, nil, nil
		}
	}

	return releaseVersion.GreaterThan(currentVersion), currentVersion, releaseVersion, nil
}

func (u *Upgrader) UpgradeSystem(ctx context.Context, client client.Client) error {
	isNewVersion, cur, release, err := u.IsNewVersionValid(ctx, client, u.DevMode)
	if err != nil {
		return err
	}

	if !isNewVersion {
		klog.Warning("It's the newest version. upgrading is unnecessary")
		return nil
	}

	u.lock.Lock()
	defer u.lock.Unlock()

	if u.running != nil && u.running.State.Cancelable() {
		return errors.New("system is upgrading now. do not repeat the upgrading")
	}

	pipelineCtx := NewCtx(ctx)
	pipelineCtx.CurrentVersion = cur
	pipelineCtx.ReleaseVersion = release
	pipelineCtx.K8sClient = client
	pipelineCtx.httpClient = &http.Client{Timeout: 2 * time.Second}

	u.running = NewPipeline(
		"system-upgrade",
		[]Job{
			&DownloadRelease{devMode: u.DevMode},
			&RunUpgradeCmd{},
		},
		pipelineCtx,
	)

	return u.running.Execute()
}

func (u *Upgrader) UpgradeState(ctx context.Context, client client.Client) (state, errMsg string) {
	if u.running == nil {
		isNewVersion, _, _, err := u.IsNewVersionValid(ctx, client, u.DevMode)
		if err != nil {
			return "none", err.Error()
		}

		if !isNewVersion {
			return "none", "it's the newest version"
		}

		return "none", "no running upgrade"
	}

	return u.running.State.String(), u.running.Error.Error()
}

func (u *Upgrader) CancelUpgrade() error {
	if u.running == nil {
		return errors.New("no running upgrade")
	}

	if !u.running.State.Cancelable() {
		return errors.New("upgrading cannot be canceled or have been canceled")
	}

	u.running.Cancel()

	return nil
}
