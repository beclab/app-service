package appstate

import appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

var StateTransitions = map[appv1alpha1.ApplicationManagerState][]appv1alpha1.ApplicationManagerState{
	appv1alpha1.Pending: {
		appv1alpha1.Downloading,
		appv1alpha1.PendingCanceling,
	},
	appv1alpha1.Downloading: {
		appv1alpha1.Installing,
		appv1alpha1.DownloadFailed,
		appv1alpha1.DownloadingCanceling,
	},
	appv1alpha1.Installing: {
		appv1alpha1.Initializing,
		appv1alpha1.InstallFailed,
		appv1alpha1.InstallingCanceling,
	},
	appv1alpha1.Initializing: {
		appv1alpha1.Running,
		//appv1alpha1.InitialFailed,
		appv1alpha1.Suspending,
		// initializing cancel operate just exit probe goroutine and turn to suspending state
		//appv1alpha1.InitializingCanceling,
	},
	appv1alpha1.Running: {
		appv1alpha1.Suspending,
		appv1alpha1.Upgrading,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.Suspending: {
		appv1alpha1.Suspended,
		appv1alpha1.SuspendFailed,
		appv1alpha1.SuspendingCanceling,
	},
	appv1alpha1.Upgrading: {
		appv1alpha1.Initializing,
		appv1alpha1.UpgradeFailed,
		appv1alpha1.UpgradingCanceling,
	},
	appv1alpha1.Uninstalling: {
		appv1alpha1.Uninstalled,
		appv1alpha1.UninstallFailed,
	},
	appv1alpha1.PendingCanceling: {
		appv1alpha1.PendingCanceled,
		appv1alpha1.PendingCancelFailed,
	},
	appv1alpha1.DownloadingCanceling: {
		appv1alpha1.DownloadingCanceled,
		appv1alpha1.DownloadingCancelFailed,
	},
	appv1alpha1.InstallingCanceling: {
		appv1alpha1.InstallingCanceled,
		appv1alpha1.InstallingCancelFailed,
	},
	// initializing state cancel directly turn to suspending

	//appv1alpha1.InitializingCanceling: {
	//	appv1alpha1.InitializingCanceled,
	//	appv1alpha1.InitializingCancelFailed,
	//},
	//appv1alpha1.SuspendingCanceling: {
	//	appv1alpha1.SuspendingCancelFailed,
	//	appv1alpha1.SuspendingCanceled,
	//},
	appv1alpha1.ResumingCanceling: {
		appv1alpha1.Suspending,
	},
	appv1alpha1.UpgradingCanceling: {
		appv1alpha1.Suspending,
	},
	appv1alpha1.Suspended: {
		appv1alpha1.Resuming,
		appv1alpha1.Uninstalling,
		appv1alpha1.Upgrading,
	},

	appv1alpha1.DownloadFailed: {
		appv1alpha1.Pending,
	},
	appv1alpha1.InstallFailed: {
		appv1alpha1.Pending,
	},
	//appv1alpha1.InitialFailed: {},
	appv1alpha1.SuspendFailed: {
		appv1alpha1.Suspending,
		appv1alpha1.Upgrading,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.UpgradeFailed: {
		appv1alpha1.Upgrading,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.ResumeFailed: {
		appv1alpha1.Resuming,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.UninstallFailed: {
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.PendingCancelFailed: {
		appv1alpha1.PendingCanceling,
	},
	appv1alpha1.DownloadingCancelFailed: {
		appv1alpha1.DownloadingCanceling,
	},
	appv1alpha1.InstallingCancelFailed: {
		appv1alpha1.InstallingCanceling,
	},
	appv1alpha1.UpgradingCancelFailed: {
		appv1alpha1.UpgradingCanceling,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.ResumingCancelFailed: {
		appv1alpha1.ResumingCanceling,
		appv1alpha1.Uninstalling,
	},
	appv1alpha1.SuspendingCancelFailed: {
		appv1alpha1.SuspendingCanceling,
		appv1alpha1.Uninstalling,
	},
}

var OperationAllowedInState = map[appv1alpha1.ApplicationManagerState]map[appv1alpha1.OpType]bool{
	// application manager does not exist
	"": {
		appv1alpha1.InstallOp: true,
	},
	appv1alpha1.Pending: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Downloading: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Installing: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Initializing: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Upgrading: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Resuming: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Suspending: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.Uninstalling:          {},
	appv1alpha1.PendingCanceling:      {},
	appv1alpha1.DownloadingCanceling:  {},
	appv1alpha1.InstallingCanceling:   {},
	appv1alpha1.InitializingCanceling: {},
	appv1alpha1.UpgradingCanceling:    {},
	appv1alpha1.ResumingCanceling:     {},
	//appv1alpha1.SuspendingCanceling:   {},

	appv1alpha1.PendingCanceled: {
		appv1alpha1.InstallOp: true,
	},
	appv1alpha1.DownloadingCanceled: {
		appv1alpha1.InstallOp: true,
	},
	appv1alpha1.InstallingCanceled: {
		appv1alpha1.InstallOp: true,
	},
	//appv1alpha1.InitializingCanceled: {
	//	appv1alpha1.UpgradeOp:   true,
	//	appv1alpha1.UninstallOp: true,
	//	appv1alpha1.ResumeOp:    true,
	//},
	appv1alpha1.UpgradingCanceled: {
		appv1alpha1.UpgradeOp:   true,
		appv1alpha1.UninstallOp: true,
		appv1alpha1.ResumeOp:    true,
	},
	appv1alpha1.ResumingCanceled: {
		appv1alpha1.UpgradeOp:   true,
		appv1alpha1.UninstallOp: true,
		appv1alpha1.ResumeOp:    true,
	},
	appv1alpha1.DownloadFailed: {
		appv1alpha1.InstallOp: true,
	},
	appv1alpha1.InstallFailed: {
		appv1alpha1.InstallOp: true,
	},
	appv1alpha1.InitialFailed: {
		appv1alpha1.UpgradeOp:   true,
		appv1alpha1.UninstallOp: true,
		appv1alpha1.ResumeOp:    true,
	},
	appv1alpha1.SuspendFailed: {
		appv1alpha1.SuspendOp:   true,
		appv1alpha1.UpgradeOp:   true,
		appv1alpha1.UninstallOp: true,
	},
	appv1alpha1.ResumeFailed: {
		appv1alpha1.ResumeOp:    true,
		appv1alpha1.UpgradeOp:   true,
		appv1alpha1.UninstallOp: true,
	},
	appv1alpha1.UninstallFailed: {
		appv1alpha1.UninstallOp: true,
	},
	appv1alpha1.UpgradeFailed: {
		appv1alpha1.UninstallOp: true,
		appv1alpha1.ResumeOp:    true,
		appv1alpha1.UpgradeOp:   true,
	},
	appv1alpha1.PendingCancelFailed: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.DownloadingCancelFailed: {
		appv1alpha1.CancelOp: true,
	},
	appv1alpha1.InstallingCancelFailed: {
		appv1alpha1.CancelOp:    true,
		appv1alpha1.UninstallOp: true,
	},
	appv1alpha1.InitializingCancelFailed: {
		appv1alpha1.CancelOp:    true,
		appv1alpha1.UninstallOp: true,
	},
	appv1alpha1.UpgradingCancelFailed: {
		appv1alpha1.CancelOp:    true,
		appv1alpha1.UninstallOp: true,
	},
}

var CancelableStates = map[appv1alpha1.ApplicationManagerState]bool{
	appv1alpha1.Pending:      true,
	appv1alpha1.Downloading:  true,
	appv1alpha1.Installing:   true,
	appv1alpha1.Initializing: true,
	appv1alpha1.Resuming:     true,
	appv1alpha1.Upgrading:    true,
}

var OperatingStates = map[appv1alpha1.ApplicationManagerState]bool{
	appv1alpha1.Pending:      true,
	appv1alpha1.Downloading:  true,
	appv1alpha1.Installing:   true,
	appv1alpha1.Initializing: true,
	appv1alpha1.Resuming:     true,
	appv1alpha1.Upgrading:    true,

	appv1alpha1.PendingCanceling:      true,
	appv1alpha1.DownloadingCanceling:  true,
	appv1alpha1.InstallingCanceling:   true,
	appv1alpha1.InitializingCanceling: true,
	appv1alpha1.ResumingCanceling:     true,
	appv1alpha1.UpgradingCanceling:    true,

	appv1alpha1.Uninstalling: true,
}

var CancelingStates = map[appv1alpha1.ApplicationManagerState]bool{
	appv1alpha1.PendingCanceling:      true,
	appv1alpha1.DownloadingCanceling:  true,
	appv1alpha1.InstallingCanceling:   true,
	appv1alpha1.InitializingCanceling: true,
	appv1alpha1.ResumingCanceling:     true,
	appv1alpha1.UpgradingCanceling:    true,
}

func IsOperationAllowed(curState appv1alpha1.ApplicationManagerState, op appv1alpha1.OpType) bool {
	if allowedOps, exists := OperationAllowedInState[curState]; exists {
		return allowedOps[op]
	}
	return false
}

func IsCancelable(curState appv1alpha1.ApplicationManagerState) bool {
	return CancelableStates[curState]
}

func IsCanceling(curState appv1alpha1.ApplicationManagerState) bool {
	return CancelingStates[curState]
}

func IsStateTransitionValid(from, to appv1alpha1.ApplicationManagerState) bool {
	if validTransitions, exists := StateTransitions[from]; exists {
		for _, validState := range validTransitions {
			if validState == to {
				return true
			}
		}
	}
	return false
}
