package v1alpha1

// ApplicationManagerState is the state of an applicationmanager at current time
type ApplicationManagerState string

// Describe the states of an applicationmanager
const (
	// Pending means that the operation is waiting to be processed.
	Pending ApplicationManagerState = "pending"

	Downloading ApplicationManagerState = "downloading"

	// Installing means that the installation operation is underway.
	Installing ApplicationManagerState = "installing"

	Initializing ApplicationManagerState = "initializing"

	Running ApplicationManagerState = "running"

	// Upgrading means that the upgrade operation is underway.
	Upgrading ApplicationManagerState = "upgrading"

	Stopping ApplicationManagerState = "stopping"

	Stopped ApplicationManagerState = "stopped"

	// Resuming means that the resume operation is underway.
	Resuming ApplicationManagerState = "resuming"

	// Uninstalling means that the uninstallation operation is underway.
	Uninstalling ApplicationManagerState = "uninstalling"

	UninstallFailed ApplicationManagerState = "uninstallFailed"

	ResumeFailed ApplicationManagerState = "resumeFailed"

	UpgradeFailed ApplicationManagerState = "upgradeFailed"

	StopFailed ApplicationManagerState = "stopFailed"

	DownloadFailed ApplicationManagerState = "downloadFailed"

	InstallFailed ApplicationManagerState = "installFailed"

	//InitialFailed ApplicationManagerState = "initialFailed"

	Uninstalled ApplicationManagerState = "uninstalled"

	// PendingCanceled means that the installation operation has been canceled.
	PendingCanceled      ApplicationManagerState = "pendingCanceled"
	DownloadingCanceled  ApplicationManagerState = "downloadingCanceled"
	InstallingCanceled   ApplicationManagerState = "installingCanceled"
	InitializingCanceled ApplicationManagerState = "initializingCanceled"
	UpgradingCanceled    ApplicationManagerState = "upgradingCanceled"
	ResumingCanceled     ApplicationManagerState = "resumingCanceled"

	// PendingCanceling means that the installation operation is under canceling operation.
	PendingCanceling      ApplicationManagerState = "pendingCanceling"
	DownloadingCanceling  ApplicationManagerState = "downloadingCanceling"
	InstallingCanceling   ApplicationManagerState = "installingCanceling"
	InitializingCanceling ApplicationManagerState = "initializingCanceling"
	UpgradingCanceling    ApplicationManagerState = "upgradingCanceling"
	ResumingCanceling     ApplicationManagerState = "resumingCanceling"
	//SuspendingCanceling   ApplicationManagerState = "suspendingCanceling"

	PendingCancelFailed     ApplicationManagerState = "pendingCancelFailed"
	DownloadingCancelFailed ApplicationManagerState = "downloadingCancelFailed"
	InstallingCancelFailed  ApplicationManagerState = "installingCancelFailed"
	//InitializingCancelFailed ApplicationManagerState = "initializingCancelFailed"
	UpgradingCancelFailed ApplicationManagerState = "upgradingCancelFailed"
	//ResumingCancelFailed  ApplicationManagerState = "resumingCancelFailed"

	//SuspendingCancelFailed ApplicationManagerState = "suspendingCancelFailed"

	// Canceling means that the cancel operation is underway.
	Canceling ApplicationManagerState = "canceling"
	Failed    ApplicationManagerState = "failed"
	Canceled  ApplicationManagerState = "canceled"
)

func (a ApplicationManagerState) String() string {
	return string(a)
}

// OpType represents the type of operation being performed.
type OpType string

// Describe the supported operation types.
const (
	// InstallOp means an installation operation.
	InstallOp OpType = "install"
	// UninstallOp means an uninstallation operation.
	UninstallOp OpType = "uninstall"
	// UpgradeOp means an upgrade operation.
	UpgradeOp OpType = "upgrade"
	// StopOp means a suspend operation.
	StopOp OpType = "stop"
	// ResumeOp means a resume operation.
	ResumeOp OpType = "resume"
	// CancelOp means a cancel operation that operation can cancel an operation at pending or installing.
	CancelOp OpType = "cancel"
)

// Type means the entity that system support.
type Type string

const (
	// App means application(crd).
	App Type = "app"
	// Recommend means argo cronworkflows.
	Recommend Type = "recommend"

	// Middleware means middleware like mongodb
	Middleware Type = "middleware"
)

func (t Type) String() string {
	return string(t)
}

// ApplicationManagerState change for app
/*
                                            +------------+     +--------------+
                         +----------------- | canceling  | --> |   canceled   |
                         |                  +------------+     +--------------+
                         |                    ^                  ^
                         |    +---------------+------------------+
                         |    |               |
+-----------+            |  +---------+     +------------+     +-----------------------------+          +----------+
| upgrading | ------+    |  | pending | --> | installing | --> |                             | -------> | resuming |
+-----------+       |    |  +---------+     +------------+     |                             |          +----------+
  |                 |    |    ^               |                |                             |                       |
  |                 |    |    +---------------+--------------- |          completed          | <---------------------+
  |                 |    |                    |                |                             |
  |                 |    |                    |                |                             |
  |                 |    |               +----+             +> |                             | -+
  |                 |    |               |                  |  +-----------------------------+  |
  |                 |    |               |                  |    ^                              |
  |                 |    +---------------+------------------+----+--------------------+         |
  |                 |                    |                  |    |                    v         |
  |                 |                    |                  |  +--------------+     +--------+  |
  |                 +--------------------+------------------+  | uninstalling | --> |        |  |
  |                                      |                     +--------------+     |        |  |
  |                                      |                       ^                  |        |  |
  |                                      +------------------+    |                  | failed |  |
  |                                                         |    |                  |        |  |
  |                                                         |    |                  |        |  |
  +---------------------------------------------------------+----+----------------> |        |  |
                                                            |    |                  +--------+  |
                                                            |    |                    ^         |
                                                            |    +--------------------+---------+
                                                            |                         |
                                                            |                         |
                                                            +-------------------------+
*/
