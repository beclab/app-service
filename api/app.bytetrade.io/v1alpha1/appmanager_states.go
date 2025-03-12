package v1alpha1

// ApplicationManagerState is the state of an applicationmanager at current time
type ApplicationManagerState string

// Describe the states of an applicationmanager
const (
	// Pending means that the operation is waiting to be processed.
	Pending ApplicationManagerState = "pending"
	// Installing means that the install operation is underway.
	Installing ApplicationManagerState = "installing"
	// Upgrading means that the upgrade operation is underway.
	Upgrading ApplicationManagerState = "upgrading"
	// Uninstalling means that the uninstall operation is underway.
	Uninstalling ApplicationManagerState = "uninstalling"
	// Canceled means that the install operation has been canceled.
	Canceled ApplicationManagerState = "canceled"
	// Failed means that the operation has failed
	Failed ApplicationManagerState = "failed"
	// Completed means that the operation has been successfully completed.
	Completed ApplicationManagerState = "completed"
	// Resuming means that the resume operation is underway.
	Resuming ApplicationManagerState = "resuming"
	// Canceling means that the cancel operation is underway.
	Canceling ApplicationManagerState = "canceling"
	// Stopping means that the suspend operation is underway.
	Stopping ApplicationManagerState = "stopping"

	Downloading ApplicationManagerState = "downloading"

	// Processing means that the intermediate state of an operation, include
	// installing,upgrading,uninstalling,resuming,canceling,stopping.
	Processing ApplicationManagerState = "processing"
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
