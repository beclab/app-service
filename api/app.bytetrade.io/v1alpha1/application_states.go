package v1alpha1

import "k8s.io/apimachinery/pkg/util/sets"

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
	AppSuspend ApplicationState = "suspended"
	// AppUninstalling means that an uninstall operation is underway.
	AppUninstalling ApplicationState = "uninstalling"
	// AppUpgrading means that an upgrade operation is underway.
	AppUpgrading ApplicationState = "upgrading"

	AppSuspending ApplicationState = "suspending"
	// AppResuming means that a resume operation is underway.
	AppResuming ApplicationState = "resuming"
	// AppDownloading in that state cr app not yet been created
	AppDownloading  ApplicationState = "downloading"
	AppInitializing ApplicationState = "initializing"
	AppCanceling    ApplicationState = "canceling"
)

var AppStateCollect = sets.NewString(AppPending.String(), AppInstalling.String(), AppRunning.String(), AppSuspend.String(), AppUninstalling.String(),
	AppUpgrading.String(), AppSuspending.String(), AppResuming.String(), AppDownloading.String(), AppInitializing.String(), AppCanceling.String())

func (a ApplicationState) String() string {
	return string(a)
}

/* ApplicationState change
+---------+  install   +-------------+     +------------+     +--------------+            +--------------+  suspend   +---------+  resume   +----------+
| pending | ---------> | downloading | --> | installing | --> | initializing | ---------> |              | ---------> | suspend | --------> | resuming |
+---------+            +-------------+     +------------+     +--------------+            |              |            +---------+           +----------+
                                                                                          |              |                                    |
                                                                +-----------------------> |   running    | <----------------------------------+
                                                                |                         |              |
                                                              +--------------+  upgrade   |              |
                                                              |  upgrading   | <--------- |              |
                                                              +--------------+            +--------------+
                                                                                            |
                                                                                            | install
                                                                                            v
                                                                                          +--------------+
                                                                                          | uninstalling |
                                                                                          +--------------+
*/
