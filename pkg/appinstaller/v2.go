package appinstaller

type InstallStrategyV2 struct {
	*InstallStrategyV1
}

var _ InstallStrategy = &InstallStrategyV2{}

func NewInstallV2(helmOps *HelmOps) InstallStrategy {
	return &InstallStrategyV2{
		InstallStrategyV1: NewInstallV1(helmOps).(*InstallStrategyV1),
	}
}

func (i *InstallStrategyV2) Install() error {
	return i.InstallStrategyV1.Install()
}

func (i *InstallStrategyV2) WaitForLaunch() (bool, error) {
	return i.WaitForLaunch()
}
