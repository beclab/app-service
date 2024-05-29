package utils

import (
	"context"
	"fmt"
	"net"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"

	"github.com/Masterminds/semver/v3"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// GetClient returns versioned ClientSet.
func GetClient() (*versioned.Clientset, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	client, err := versioned.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// UpdateAppState update application status state.
func UpdateAppState(appmgr *v1alpha1.ApplicationManager, state v1alpha1.ApplicationState) error {
	client, err := GetClient()
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		app, err := client.AppV1alpha1().Applications().Get(context.TODO(), appmgr.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// dev mode, try to find app in user-space
				apps, err := client.AppV1alpha1().Applications().List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return err
				}

				for _, a := range apps.Items {
					if a.Spec.Name == appmgr.Spec.AppName &&
						a.Spec.Owner == appmgr.Spec.AppOwner &&
						a.Spec.Namespace == "user-space-"+a.Spec.Owner {
						app = &a

						break
					}
				}

			} else {
				return err
			}
		}
		now := metav1.Now()
		appCopy := app.DeepCopy()
		appCopy.Status.State = state.String()
		appCopy.Status.StatusTime = &now
		appCopy.Status.UpdateTime = &now

		if appCopy.Name == "" {
			return nil
		}

		_, err = client.AppV1alpha1().Applications().Get(context.TODO(), appCopy.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		_, err = client.AppV1alpha1().Applications().UpdateStatus(context.TODO(), appCopy, metav1.UpdateOptions{})

		return err
	})

}

// UpdateAppMgrStatus update applicationmanager status, if filed in parameter status is empty that field will not be set.
func UpdateAppMgrStatus(name string, status v1alpha1.ApplicationManagerStatus) (*v1alpha1.ApplicationManager, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}
	var appMgr *v1alpha1.ApplicationManager

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		appMgr, err = client.AppV1alpha1().ApplicationManagers().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		appMgrCopy := appMgr.DeepCopy()

		status.OpGeneration = appMgrCopy.Status.OpGeneration + 1
		status.OpRecords = appMgrCopy.Status.OpRecords

		if status.State == "" {
			status.State = appMgrCopy.Status.State
		}
		if status.Message == "" {
			status.Message = appMgrCopy.Status.Message
		}
		payload := status.Payload
		if payload == nil {
			payload = make(map[string]string)
		}
		for k, v := range appMgrCopy.Status.Payload {
			if _, ok := payload[k]; !ok {
				payload[k] = v
			}
		}
		status.Payload = payload

		appMgrCopy.Status = status

		appMgr, err = client.AppV1alpha1().ApplicationManagers().UpdateStatus(context.TODO(), appMgrCopy, metav1.UpdateOptions{})
		return err
	})

	return appMgr, err
}

// GetDeployedReleaseVersion check whether app has been deployed and return release chart version
func GetDeployedReleaseVersion(actionConfig *action.Configuration, appName string) (string, int, error) {
	client := action.NewGet(actionConfig)
	release, err := client.Run(appName)
	if err != nil {
		return "", 0, err
	}
	return release.Chart.Metadata.Version, release.Version, nil
}

// MatchVersion check if the version satisfies the constraint.
func MatchVersion(version, constraint string) bool {
	if len(version) == 0 {
		return true
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		klog.Errorf("Invalid constraint=%s err=%v, ", constraint, err)
		return false
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		klog.Errorf("Invalid version=%s err=%v", version, err)
		return false
	}

	return c.Check(v)
}

// CreateSysAppMgr create an applicationmanager for the system application.
func CreateSysAppMgr(app, owner string) error {
	client, err := GetClient()
	if err != nil {
		return err
	}
	now := metav1.Now()
	appMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: FmtAppMgrName(app, owner),
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:      app,
			AppNamespace: AppNamespace(app, owner),
			AppOwner:     owner,
			Source:       "system",
		},
	}
	a, err := client.AppV1alpha1().ApplicationManagers().Create(context.TODO(), appMgr, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	appMgrCopy := a.DeepCopy()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:       v1alpha1.InstallOp,
		State:        v1alpha1.Completed,
		OpGeneration: int64(0),
		Message:      "sys app install completed",
		UpdateTime:   &now,
		StatusTime:   &now,
	}
	appMgrCopy.Status = status
	_, err = client.AppV1alpha1().ApplicationManagers().UpdateStatus(context.TODO(), appMgrCopy, metav1.UpdateOptions{})

	return err
}

// GetAppMgrStatus returns status of an applicationmanager.
func GetAppMgrStatus(name string) (*v1alpha1.ApplicationManagerStatus, error) {
	client, err := GetClient()
	if err != nil {
		return nil, err
	}
	appMgr, err := client.AppV1alpha1().ApplicationManagers().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &appMgr.Status, nil
}

// AppNamespace returns the namespace of an application.
func AppNamespace(app, owner string) string {
	if userspace.IsSysApp(app) {
		app = "user-space"
	}
	return fmt.Sprintf("%s-%s", app, owner)
}

// FmtAppMgrName returns applicationmanager name for application.
func FmtAppMgrName(app, owner string) string {
	namespace := AppNamespace(app, owner)
	return fmt.Sprintf("%s-%s", namespace, app)
}

// FmtModelMgrName returns applicationmanager name for model.
func FmtModelMgrName(modelID string) string {
	return modelID
}

// TryConnect try to connect to a service with specified host and port.
func TryConnect(host string, port string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		klog.Errorf("Try to connect: %s:%s err=%v", host, port, err)
		return false
	}
	if conn != nil {
		defer conn.Close()
		return true
	}

	return false
}

// GetAppID returns appID for an application.
// for system app appID equals name, otherwise appID equals md5(name)[:8].
func GetAppID(name string) string {
	if userspace.IsSysApp(name) {
		return name
	}
	return Md5String(name)[:8]
}

// GetPendingOrRunningTask returns pending and running state applicationmanager.
func GetPendingOrRunningTask(ctx context.Context) (ams []v1alpha1.ApplicationManager, err error) {
	ams = make([]v1alpha1.ApplicationManager, 0)
	client, err := GetClient()
	if err != nil {
		return ams, err
	}
	list, err := client.AppV1alpha1().ApplicationManagers().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ams, err
	}
	for _, am := range list.Items {
		if am.Status.State == v1alpha1.Pending || am.Status.State == v1alpha1.Installing ||
			am.Status.State == v1alpha1.Uninstalling || am.Status.State == v1alpha1.Upgrading {
			ams = append(ams, am)
		}
	}

	return ams, nil
}

// UpdateStatus update application state and applicationmanager state.
func UpdateStatus(appMgr *v1alpha1.ApplicationManager, state v1alpha1.ApplicationManagerState,
	opRecord *v1alpha1.OpRecord, appState v1alpha1.ApplicationState, message string) error {
	client, _ := GetClient()
	var err error
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		appMgr, err = client.AppV1alpha1().ApplicationManagers().Get(context.TODO(), appMgr.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		now := metav1.Now()
		appMgrCopy := appMgr.DeepCopy()
		appMgrCopy.Status.State = state
		appMgrCopy.Status.Message = message
		appMgrCopy.Status.StatusTime = &now
		appMgrCopy.Status.UpdateTime = &now
		if opRecord != nil {
			appMgrCopy.Status.OpRecords = append([]v1alpha1.OpRecord{*opRecord}, appMgr.Status.OpRecords...)
		}
		if len(appMgr.Status.OpRecords) > 20 {
			appMgrCopy.Status.OpRecords = appMgr.Status.OpRecords[:20:20]
		}
		//klog.Infof("utils: UpdateStatus: %v", appMgrCopy.Status.Conditions)

		_, err = client.AppV1alpha1().ApplicationManagers().UpdateStatus(context.TODO(), appMgrCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		if len(appState) > 0 {
			err = UpdateAppState(appMgr, appState)
			if err != nil {
				return err
			}
		}
		return err
	})
}
