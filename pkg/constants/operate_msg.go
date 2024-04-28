package constants

// describes the template for operation to record operate history.
const (
	// InstallOperationCompletedTpl is for successful install operation.
	InstallOperationCompletedTpl = "Successfully installed %s: %s"
	// OperationCanceledByUserTpl is for cancel operation by user.
	OperationCanceledByUserTpl = "Install canceled by user."
	// OperationCanceledByTerminusTpl is for cancel operation by system.
	OperationCanceledByTerminusTpl = "Install canceled. Operation timed out."
	// UninstallOperationCompletedTpl is for successful uninstall operation.
	UninstallOperationCompletedTpl = "Successfully uninstalled %s: %s"
	// UpgradeOperationCompletedTpl is for successful upgrade operation.
	UpgradeOperationCompletedTpl = "Successfully upgraded %s: %s"
	// SuspendOperationCompletedTpl is for suspend operation.
	SuspendOperationCompletedTpl = "%s suspended."
	// SuspendOperationForModelCompletedTpl is for successful unload operation.
	SuspendOperationForModelCompletedTpl = "%s unloaded."
	// SuspendOperationForModelFailedTpl is for failed upload operation.
	SuspendOperationForModelFailedTpl = "Failed to unload: %s"
	// ResumeOperationCompletedTpl is for successful resume operation.
	ResumeOperationCompletedTpl = "%s resumed."
	// ResumeOperationForModelCompletedTpl is for successful load operation.
	ResumeOperationForModelCompletedTpl = "%s loaded."
	// ResumeOperationForModelFailedTpl is for failed load operation.
	ResumeOperationForModelFailedTpl = "Failed to load: %s"

	// OperationFailedTpl is for failed opration.
	OperationFailedTpl = "Failed to %s: %s"
)
