package v1alpha1

// ApplicationManagerState is the state of an applicationmanager at current time
type ApplicationManagerState string

// Describe the states of an applicationmanager
const (
	// Pending means that the operation is waiting to be processed.
	Pending ApplicationManagerState = "pending"

	Downloading ApplicationManagerState = "downloading"

	// Installing means that the install operation is underway.
	Installing ApplicationManagerState = "installing"

	Initializing ApplicationManagerState = "initializing"

	Running ApplicationManagerState = "running"

	// Upgrading means that the upgrade operation is underway.
	Upgrading ApplicationManagerState = "upgrading"

	Suspending ApplicationManagerState = "suspending"

	Suspended ApplicationManagerState = "suspended"

	// Resuming means that the resume operation is underway.
	Resuming ApplicationManagerState = "resuming"

	// Uninstalling means that the uninstall operation is underway.
	Uninstalling ApplicationManagerState = "uninstalling"

	CancelFailed ApplicationManagerState = "cancelFailed"

	UninstallFailed ApplicationManagerState = "uninstallFailed"

	ResumeFailed ApplicationManagerState = "resumeFailed"

	UpgradeFailed ApplicationManagerState = "upgradeFailed"

	SuspendFailed ApplicationManagerState = "suspendFailed"

	DownloadFailed ApplicationManagerState = "downloadFailed"

	InstallFailed ApplicationManagerState = "installFailed"

	InitialFailed ApplicationManagerState = "initialFailed"

	Uninstalled ApplicationManagerState = "uninstalled"

	// PendingCanceled means that the installation operation has been canceled.
	PendingCanceled ApplicationManagerState = "pendingCanceled"

	DownloadingCanceled  ApplicationManagerState = "downloadingCanceled"
	InstallingCanceled   ApplicationManagerState = "installingCanceled"
	InitializingCanceled ApplicationManagerState = "initializingCanceled"
	UpgradingCanceled    ApplicationManagerState = "upgradingCanceled"
	ResumingCanceled     ApplicationManagerState = "resumingCanceled"

	// Canceling means that the cancel operation is underway.
	Canceling ApplicationManagerState = "canceling"
)

func (a ApplicationManagerState) String() string {
	return string(a)
}

// OpType represents the type of operation being performed.
type OpType string

// Describe the supported operation types.
const (
	// InstallOp means an install operation.
	InstallOp OpType = "install"
	// UninstallOp means an uninstall operation.
	UninstallOp OpType = "uninstall"
	// UpgradeOp means an upgrade operation.
	UpgradeOp OpType = "upgrade"
	// SuspendOp means a suspend operation.
	SuspendOp OpType = "suspend"
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
