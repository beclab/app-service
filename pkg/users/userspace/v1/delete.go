package userspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/users/userspace/templates"
	"bytetrade.io/web3os/app-service/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	checkLauncherRunning  = "Running"
	checkLauncherNoExists = "NotExists"
)

type Deleter struct {
	clientSet *clientset.ClientSet // k8s session client
	k8sConfig *rest.Config         // k8s service account config
	user      string
	helmCfg   helm.Config
}

func NewDeleter(client *clientset.ClientSet, config *rest.Config, user string) *Deleter {
	return &Deleter{
		clientSet: client,
		k8sConfig: config,
		user:      user,
	}
}

func (d *Deleter) DeleteUserApps(ctx context.Context) error {
	err := d.uninstallUserApps(ctx)
	if err != nil {
		return err
	}

	userspaceName := utils.UserspaceName(d.user)

	actionCfg, settings, err := helm.InitConfig(d.k8sConfig, userspaceName)

	if err != nil {
		return err
	}
	d.helmCfg.ActionCfg = actionCfg
	d.helmCfg.Settings = settings

	err = d.uninstallSysApps(ctx)
	if err != nil {
		return err
	}

	launcherReleaseName := helm.ReleaseName("launcher", d.user)

	err = d.clearLauncher(ctx, launcherReleaseName, userspaceName)
	if err != nil {
		return err
	}

	userspaceRoleBinding := templates.NewUserspaceRoleBinding(d.user, userspaceName, USER_SPACE_ROLE)
	return d.deleteNamespace(ctx, userspaceName, userspaceRoleBinding.Name)
}

func (d *Deleter) deleteNamespace(ctx context.Context, userspace, userspaceRoleBinding string) error {
	// err := d.clientSet.KubeClient.Kubernetes().
	// 	CoreV1().Namespaces().Delete(ctx, userspace, metav1.DeleteOptions{})
	// if err != nil {
	// 	return err
	// }

	// err = d.clientSet.KubeClient.Kubernetes().
	// 	RbacV1().RoleBindings(userspace).Delete(ctx, userspaceRoleBinding, metav1.DeleteOptions{})
	// if err != nil {
	// 	return err
	// }

	usersystem := templates.NewUserSystem(d.user)
	err := d.clientSet.KubeClient.Kubernetes().
		CoreV1().Namespaces().Delete(ctx, usersystem.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	usersystemRoleBinding := templates.NewUserspaceRoleBinding(d.user, usersystem.Name, USER_SPACE_ROLE)
	err = d.clientSet.KubeClient.Kubernetes().
		RbacV1().RoleBindings(usersystem.Name).Delete(ctx, usersystemRoleBinding.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	return err
}

func (d *Deleter) clearLauncher(ctx context.Context, launchername, userspace string) error {
	// get pvcs that need clear from bfl sts
	// cleanPvc, err := d.findPVC(ctx, userspace)
	// if err != nil {
	// 	return err
	// }

	err := helm.UninstallCharts(d.helmCfg.ActionCfg, launchername)
	if err != nil {
		return err
	}

	if _, err := d.checkLauncher(ctx, userspace, checkLauncherNoExists); err != nil {
		return err
	}

	// clear pvc
	// if cleanPvc != nil {
	// 	var errs []error
	// 	errs = append(errs,
	// 		d.clientSet.KubeClient.Kubernetes().CoreV1().PersistentVolumeClaims(userspace).
	// 			Delete(ctx, cleanPvc.userspacePvc, metav1.DeleteOptions{}))
	// 	errs = append(errs,
	// 		d.clientSet.KubeClient.Kubernetes().CoreV1().PersistentVolumeClaims(userspace).
	// 			Delete(ctx, cleanPvc.appdataPvc, metav1.DeleteOptions{}))
	// 	errs = append(errs,
	// 		d.clientSet.KubeClient.Kubernetes().CoreV1().PersistentVolumeClaims(userspace).
	// 			Delete(ctx, cleanPvc.dbdataPvc, metav1.DeleteOptions{}))

	// 	if len(errs) > 0 {
	// 		return utilerrors.NewAggregate(errs)
	// 	}
	// }
	return nil
}

func (d *Deleter) findPVC(ctx context.Context, userspace string) (
	res *struct{ userspacePvc, userspacePv, appCachePvc, appCacheHostPath, dbdataPvc, dbdataHostPath string },
	err error) {
	sts, err := d.clientSet.KubeClient.Kubernetes().AppsV1().StatefulSets(userspace).Get(ctx, "bfl", metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get sts bfl userspace=%s err=%v", userspace, err)
		return nil, err
	}

	var ok bool
	res = &struct{ userspacePvc, userspacePv, appCachePvc, appCacheHostPath, dbdataPvc, dbdataHostPath string }{}
	res.userspacePvc, ok = sts.Annotations["userspace_pvc"]
	if !ok {
		return nil, errors.New("userspace PVC not found")
	}

	res.userspacePv, ok = sts.Annotations["userspace_pv"]
	if !ok {
		return nil, errors.New("userspace PV not found")
	}

	res.appCachePvc, ok = sts.Annotations["appcache_pvc"]
	if !ok {
		return nil, errors.New("appcache PVC not found")
	}

	res.appCacheHostPath, ok = sts.Annotations["appcache_hostpath"]
	if !ok {
		return nil, errors.New("appcache PVC not found")
	}

	res.dbdataPvc, ok = sts.Annotations["dbdata_pvc"]
	if !ok {
		return nil, errors.New("dbdata PVC not found")
	}

	res.dbdataHostPath, ok = sts.Annotations["dbdata_hostpath"]
	if !ok {
		return nil, errors.New("dbdata PVC not found")
	}

	return res, nil
}

func (d *Deleter) uninstallUserApps(ctx context.Context) error {
	applist, err := d.clientSet.AppClient.AppV1alpha1().Applications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	// filter by application's owner
	for _, a := range applist.Items {
		if a.Spec.Owner == d.user && !d.isSysApps(&a) {
			actionCfg, _, err := helm.InitConfig(d.k8sConfig, a.Spec.Namespace)
			if err != nil {
				klog.Errorf("Failed to delete user's application on config init, owner=%s err=%v", a.Spec.Owner, err)
				continue
			}

			err = helm.UninstallCharts(actionCfg, a.Spec.Name)
			if err != nil {
				klog.Errorf("Failed to delete user's application owner=%s, err=%v", a.Spec.Owner, err)
			}
			name := fmt.Sprintf("%s-%s-%s", a.Spec.Name, a.Spec.Owner, a.Spec.Name)
			err = d.clientSet.AppClient.AppV1alpha1().ApplicationManagers().Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("Failed to delete user's applicationmanager name=%s owner=%s err=%v", name, a.Spec.Owner, err)
			}
			err = d.clientSet.KubeClient.Kubernetes().CoreV1().Namespaces().Delete(ctx, a.Spec.Namespace, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("Failed to delete user's namespace=%s err=%v", a.Spec.Namespace, err)
			}
		}
	}

	return nil
}

func (d *Deleter) uninstallSysApps(ctx context.Context) error {
	sysApps, err := userspace.GetAppsFromDirectory(constants.UserChartsPath + "/apps")
	if err != nil {
		return err
	}

	var errsCount int

	for _, appname := range sysApps {
		appReleaseName := helm.ReleaseName(appname, d.user)
		err = helm.UninstallCharts(d.helmCfg.ActionCfg, appReleaseName)
		if err != nil {
			klog.Errorf("Failed to uninstall chart user=%s appName=%s err=%v", d.user, appname, err)
			errsCount++
		}

	}
	appmgrs, err := d.clientSet.AppClient.AppV1alpha1().ApplicationManagers().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, am := range appmgrs.Items {
		if am.Spec.AppOwner == d.user {
			err = d.clientSet.AppClient.AppV1alpha1().ApplicationManagers().Delete(ctx, am.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("Failed to delete user's sys applicationmanager user=%s err=%v", d.user, err)
			}
		}
	}
	if errsCount == len(sysApps) {
		return errors.New("uninstall all sys apps error")
	}
	return nil
}

func (d *Deleter) checkLauncher(ctx context.Context, userspace string, runningOrExists string) (*corev1.Pod, error) {
	var (
		bfl          *corev1.Pod
		observations int
	)

	err := wait.PollImmediate(time.Second, 5*time.Minute, func() (bool, error) {
		pods, err := d.clientSet.KubeClient.Kubernetes().CoreV1().Pods(userspace).
			List(ctx, metav1.ListOptions{LabelSelector: "tier=bfl"})

		if err != nil {
			return true, err
		}

		// check not exists
		if len(pods.Items) == 0 && runningOrExists == checkLauncherNoExists {
			return true, nil
		}

		if err == nil && len(pods.Items) > 0 {
			observations++
		}

		if observations >= 2 {
			if pods != nil && len(pods.Items) > 0 {
				pod := pods.Items[0]
				// check bfl is running, and return the bfl
				if pod.Status.Phase == corev1.PodRunning && runningOrExists == checkLauncherRunning {
					bfl = &pod
					return true, nil
				}
			}
		}
		return false, nil
	})

	return bfl, err
}

func (d *Deleter) isSysApps(app *appv1alpha1.Application) bool {
	userspace := utils.UserspaceName(d.user)
	return app.Spec.Namespace == userspace
}
