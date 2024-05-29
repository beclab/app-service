package v1alpha1

// ApplicationState is the state of an application at current time
type ApplicationState string

// These ar the valid states of applications
const (
	// AppPending means that the application has been accepted by the system, but system has
	// at least one application is still at 'installing'/'upgrading' state.
	AppPending ApplicationState = "pending"
	// AppInstalling means that the application has been start to install by the system.
	AppInstalling ApplicationState = "installing"
	// AppRunning means that the application is installed success and ready for serve.
	AppRunning ApplicationState = "running"
	// AppSuspend means that the application's deployment/statefulset replicas has been set to zero.
	AppSuspend ApplicationState = "suspend"
	// AppUninstalling means that an uninstall operation is underway.
	AppUninstalling ApplicationState = "uninstalling"
	// AppUpgrading means that an upgrade operation is underway.
	AppUpgrading ApplicationState = "upgrading"
	// AppResuming means that a resume operation is underway.
	AppResuming    ApplicationState = "resuming"
	AppDownloading ApplicationState = "downloading"
)

func (a ApplicationState) String() string {
	return string(a)
}

/* ApplicationState change
+---------+     +------------+     +--------------+     +---------+     +----------+
| pending | --> | installing | --> |              | --> | suspend | --> | resuming |
+---------+     +------------+     |              |     +---------+     +----------+
                                   |              |                       |
                  +--------------> |   running    | <---------------------+
                  |                |              |
                +------------+     |              |
                | upgrading  | <-- |              |
                +------------+     +--------------+
                                     |
                                     |
                                     v
                                   +--------------+
                                   | uninstalling |
                                   +--------------+
*/
