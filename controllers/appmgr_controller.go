package controllers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/task"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/go-resty/resty/v2"
	"helm.sh/helm/v3/pkg/action"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const suspendAnnotation = "bytetrade.io/suspend-by"
const suspendCauseAnnotation = "bytetrade.io/suspend-cause"

// ApplicationManagerController represents a controller for managing the lifecycle of applicationmanager.
type ApplicationManagerController struct {
	client.Client
}

var middlewareTypes = []string{tapr.TypePostgreSQL.String(), tapr.TypeMongoDB.String(), tapr.TypeRedis.String(), tapr.TypeNats.String()}

// SetupWithManager sets up the ApplicationManagerController with the provided controller manager
func (r *ApplicationManagerController) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("appmgr-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}

	v := reflect.ValueOf(c.(controller.Controller)).Elem()
	value := v.FieldByName("MakeQueue")
	makeFunc := func() workqueue.RateLimitingInterface {
		return task.WQueue
	}
	value.Set(reflect.ValueOf(makeFunc))

	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.ApplicationManager{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				app, ok := h.(*appv1alpha1.ApplicationManager)
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      app.Name,
					Namespace: app.Spec.AppOwner,
				}}}
			}),
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.preEnqueueCheckForCreate(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.preEnqueueCheckForUpdate(e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
		},
	)

	if err != nil {
		klog.Errorf("Failed to add watch err=%v", err)
		return nil
	}

	return nil
}

var manager map[string]context.CancelFunc

// Reconcile implements the reconciliation loop for the ApplicationManagerController
func (r *ApplicationManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	ctrl.Log.Info("reconcile application manager request", "name", req.Name)

	var appMgr appv1alpha1.ApplicationManager
	err := r.Get(ctx, req.NamespacedName, &appMgr)

	if err != nil {
		ctrl.Log.Error(err, "get application manager error", "name", req.Name)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// unexpected error, retry after 5s
		return ctrl.Result{}, err
	}

	go func() {
		r.reconcile(&appMgr)
	}()

	return reconcile.Result{}, nil
}

func (r *ApplicationManagerController) reconcile(instance *appv1alpha1.ApplicationManager) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defer func() {
		delete(manager, instance.Name)
		req := reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: instance.Spec.AppOwner,
		}}
		task.WQueue.(*task.Type).SetCompleted(req)
	}()
	var err error
	klog.Infof("Start to perform operate=%s appName=%s", instance.Status.OpType, instance.Spec.AppName)

	var curAppMgr appv1alpha1.ApplicationManager
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name}, &curAppMgr)
	if err != nil {
		klog.Errorf("Failed to get applicationmanagers name=%s err=%v", instance.Name, err)
		now := metav1.Now()
		version := "0.0.1"
		if instance.Status.Payload != nil {
			version = instance.Status.Payload["version"]
		}
		message := fmt.Sprintf(constants.OperationFailedTpl, instance.Status.OpType, err.Error())
		opRecord := appv1alpha1.OpRecord{
			OpType:     instance.Status.OpType,
			Message:    message,
			Source:     instance.Spec.Source,
			Version:    version,
			Status:     appv1alpha1.Failed,
			StatusTime: &now,
		}
		e := r.updateStatus(instance, appv1alpha1.Failed, &opRecord, "", message)
		if e != nil {
			klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", instance.Name, e)
		}
		return
	}
	switch instance.Status.OpType {
	case appv1alpha1.InstallOp:
		if manager == nil {
			manager = make(map[string]context.CancelFunc)
		}
		manager[instance.Name] = cancel

		if curAppMgr.Status.State == appv1alpha1.Canceled {
			return
		}
		err = r.install(ctx, instance)
	case appv1alpha1.UpgradeOp:
		if curAppMgr.Status.State == appv1alpha1.Canceled {
			return
		}
		err = r.upgrade(ctx, instance)
	default:
	}

	if err != nil {
		klog.Errorf("Failed to perform operate=%s app=%s err=%v", instance.Status.OpType, instance.Spec.AppName, err)
	} else {
		klog.Infof("Success to perform operate=%s app=%s", instance.Status.OpType, instance.Spec.AppName)
	}

	return
}

func (r *ApplicationManagerController) reconcile2(cur *appv1alpha1.ApplicationManager) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	klog.Infof("Start to perform operate=%s app=%s", cur.Status.OpType, cur.Spec.AppName)
	switch cur.Status.OpType {
	case appv1alpha1.UninstallOp:
		err = r.uninstall(ctx, cur)
	case appv1alpha1.CancelOp:
		if cur.Status.State == appv1alpha1.Installing || cur.Status.State == appv1alpha1.Downloading {
			err = r.cancel(cur)
		}
		if cur.Status.State == appv1alpha1.Pending {
			err = r.updateStatus(cur, appv1alpha1.Canceled, nil, "", constants.OperationCanceledByUserTpl)
			if err != nil {
				klog.Info("Failed to update applicationmanagers status name=%s err=%v", cur.Name, err)
			}
		}

	case appv1alpha1.SuspendOp:
		err = r.suspend(cur, cur.Spec.AppNamespace)
	case appv1alpha1.ResumeOp:
		err = r.resumeAppAndWaitForLaunch(cur)
	default:
	}

	if err != nil {
		klog.Errorf("Failed to perform operate=%s app=%s err=%v", cur.Status.OpType, cur.Spec.AppName, err)
	} else {
		klog.Infof("Success to perform operate=%s app=%s", cur.Status.OpType, cur.Spec.AppName)
	}

}

func (r *ApplicationManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	cur, _ := obj.(*appv1alpha1.ApplicationManager)
	if cur.Spec.Type != appv1alpha1.App {
		return false
	}

	state := cur.Status.State
	opType := cur.Status.OpType

	// if applicationmanager state is in (completed, canceled, failed, "") skip
	if state == "" || state == appv1alpha1.Completed || state == appv1alpha1.Canceled || state == appv1alpha1.Failed {
		return false
	}

	if opType == appv1alpha1.UninstallOp || opType == appv1alpha1.CancelOp ||
		opType == appv1alpha1.SuspendOp || opType == appv1alpha1.ResumeOp {
		go r.reconcile2(cur)
		return false
	}
	return true
}

func (r *ApplicationManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	oldAppMgr, _ := old.(*appv1alpha1.ApplicationManager)
	curAppMgr, _ := new.(*appv1alpha1.ApplicationManager)
	opType := curAppMgr.Status.OpType

	if curAppMgr.Spec.Type != appv1alpha1.App {
		return false
	}

	if curAppMgr.Status.OpGeneration <= oldAppMgr.Status.OpGeneration {
		return false
	}

	if opType == appv1alpha1.UninstallOp || opType == appv1alpha1.CancelOp ||
		opType == appv1alpha1.SuspendOp || opType == appv1alpha1.ResumeOp {
		go r.reconcile2(curAppMgr)
		return false
	}

	return true
}

func (r *ApplicationManagerController) updateStatus(appMgr *appv1alpha1.ApplicationManager, state appv1alpha1.ApplicationManagerState,
	opRecord *appv1alpha1.OpRecord, appState appv1alpha1.ApplicationState, message string) error {
	var err error
	now := metav1.Now()
	appMgrCopy := appMgr.DeepCopy()
	appMgr.Status.State = state
	appMgr.Status.Message = message
	appMgr.Status.StatusTime = &now
	appMgr.Status.UpdateTime = &now
	if opRecord != nil {
		appMgr.Status.OpRecords = append([]appv1alpha1.OpRecord{*opRecord}, appMgr.Status.OpRecords...)
	}
	if len(appMgr.Status.OpRecords) > 20 {
		appMgr.Status.OpRecords = appMgr.Status.OpRecords[:20:20]
	}
	err = r.Status().Patch(context.TODO(), appMgr, client.MergeFrom(appMgrCopy))
	if err != nil {
		return err
	}
	if len(appState) > 0 {
		err = utils.UpdateAppState(appMgr, appState)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ApplicationManagerController) install(ctx context.Context, appMgr *appv1alpha1.ApplicationManager) (err error) {
	var version string
	var ops *appinstaller.HelmOps
	defer func() {
		if err != nil {
			if err.Error() == "canceled" {
				return
			}
			state := appv1alpha1.Failed
			now := metav1.Now()
			message := err.Error()
			var appMgrCur appv1alpha1.ApplicationManager
			e := r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &appMgrCur)
			if e != nil {
				klog.Errorf("Failed to get applicationmanagers name=%s err=%v", appMgr.Name, e)
			}

			if ops != nil {
				e = ops.Uninstall()
				if e != nil {
					klog.Errorf("Failed to uninstall app name=%s err=%v", appMgr.Spec.AppName, e)
				}
			}

			opRecord := appv1alpha1.OpRecord{
				OpType:     appv1alpha1.InstallOp,
				Message:    message,
				Source:     appMgr.Spec.Source,
				Version:    version,
				Status:     state,
				StatusTime: &now,
			}
			if errors.Is(err, context.Canceled) {
				opRecord.Status = appv1alpha1.Canceled
				opRecord.OpType = appv1alpha1.CancelOp
				opRecord.Message = constants.OperationCanceledByUserTpl
			}
			e = r.updateStatus(appMgr, opRecord.Status, &opRecord, "", opRecord.Message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
			}
		}

	}()
	payload := appMgr.Status.Payload
	version = payload["version"]
	token := payload["token"]
	cfgURL := payload["cfgURL"]
	repoURL := payload["repoURL"]

	var appconfig *appinstaller.ApplicationConfig
	var chartPath string
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	appconfig, chartPath, err = apiserver.GetAppConfig(ctx, appMgr.Spec.AppName, appMgr.Spec.AppOwner, cfgURL, repoURL, "", token)
	if err != nil {
		return err
	}
	err = setExposePorts(appconfig)
	if err != nil {
		return err
	}

	ops, err = appinstaller.NewHelmOps(ctx, kubeConfig, appconfig, token, appinstaller.Opt{Source: appMgr.Spec.Source})
	if err != nil {
		return err
	}
	// get images that need to download
	refs, err := utils.GetRefFromResourceList(chartPath)
	if err != nil {
		klog.Infof("get ref err=%v", err)
		return err
	}

	err = r.createImageManager(ctx, appMgr, refs)
	if err != nil {
		return err
	}

	// wait image to be downloaded
	err = r.updateStatus(appMgr, appv1alpha1.Downloading, nil, "", "downloading")
	if err != nil {
		return err
	}

	err = r.pollDownloadProgress(ctx, appMgr)

	if err != nil {
		return err
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		klog.Infof("update im status")
		return r.updateImStatus(ctx, appMgr.Name, appv1alpha1.Completed.String(), "success")
	})
	if err != nil {
		return err
	}

	err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, appMgr)
	if err != nil {
		return err
	}

	// this time app has not been created, so do not update app status
	message := fmt.Sprintf("Start to install %s: %s", appMgr.Spec.Type.String(), appMgr.Spec.AppName)
	err = r.updateStatus(appMgr, appv1alpha1.Installing, nil, "", message)
	if err != nil {
		return err
	}

	_, err = apiserver.CheckAppRequirement(kubeConfig, token, appconfig)
	if err != nil {
		return err
	}

	_, err = apiserver.CheckUserResRequirement(ctx, kubeConfig, appconfig, appMgr.Spec.AppOwner)
	if err != nil {
		return err
	}
	err = ops.Install()
	if err != nil {
		return err
	}

	now := metav1.Now()
	message = fmt.Sprintf(constants.InstallOperationCompletedTpl, appMgr.Spec.Type.String(), appMgr.Spec.AppName)
	opRecord := appv1alpha1.OpRecord{
		OpType:     appv1alpha1.InstallOp,
		Message:    message,
		Source:     appMgr.Spec.Source,
		Version:    version,
		Status:     appv1alpha1.Completed,
		StatusTime: &now,
	}
	err = r.updateStatus(appMgr, appv1alpha1.Completed, &opRecord, appv1alpha1.AppRunning, message)
	if err != nil {
		klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, err)
	}

	err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, appMgr)
	var curAppMgr appv1alpha1.ApplicationManager
	err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &curAppMgr)
	return err
}

func (r *ApplicationManagerController) uninstall(ctx context.Context, appMgr *appv1alpha1.ApplicationManager) (err error) {
	var version string
	defer func() {
		if err != nil {
			now := metav1.Now()
			message := fmt.Sprintf(constants.OperationFailedTpl, appMgr.Status.OpType, err.Error())
			opRecord := appv1alpha1.OpRecord{
				OpType:     appv1alpha1.UninstallOp,
				Message:    message,
				Source:     appMgr.Spec.Source,
				Version:    version,
				Status:     appv1alpha1.Failed,
				StatusTime: &now,
			}
			e := r.updateStatus(appMgr, appv1alpha1.Failed, &opRecord, "", message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
			}
		}
	}()
	err = r.updateStatus(appMgr, appv1alpha1.Uninstalling, nil, appv1alpha1.AppUninstalling, appv1alpha1.AppUninstalling.String())
	if err != nil {
		return err
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	token := appMgr.Status.Payload["token"]
	version = appMgr.Status.Payload["version"]
	appConfig := &appinstaller.ApplicationConfig{
		AppName:   appMgr.Spec.AppName,
		Namespace: appMgr.Spec.AppNamespace,
		OwnerName: appMgr.Spec.AppOwner,
	}
	ops, err := appinstaller.NewHelmOps(ctx, config, appConfig, token, appinstaller.Opt{})

	appCacheDirs, _ := tryToGetAppdataDirFromDeployment(ctx, appConfig, appMgr.Spec.AppOwner)
	err = ops.Uninstall()
	if err != nil {
		return err
	}

	// delete middleware requests crd
	namespace := fmt.Sprintf("%s-%s", "user-system", appConfig.OwnerName)
	for _, mt := range middlewareTypes {
		name := fmt.Sprintf("%s-%s", appConfig.AppName, mt)
		err = tapr.DeleteMiddlewareRequest(ctx, config, namespace, name)
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete middleware request namespace=%s name=%s err=%v", namespace, name, err)
		}
	}

	if len(appCacheDirs) > 0 {
		terminusNonce, e := utils.GenTerminusNonce()
		if e != nil {
			klog.Errorf("Failed to generate terminus nonce err=%v", e)
		} else {
			c := resty.New().SetTimeout(2*time.Second).
				SetHeader(constants.AuthorizationTokenKey, token).
				SetHeader("Terminus-Nonce", terminusNonce)
			nodes, e := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			if e == nil {
				for _, dir := range appCacheDirs {
					for _, n := range nodes.Items {
						URL := fmt.Sprintf(constants.AppDataDirURL, appMgr.Spec.AppOwner, dir)
						c.SetHeader("X-Terminus-Node", n.Name)
						c.SetHeader("x-bfl-user", appMgr.Spec.AppOwner)
						res, e := c.R().Delete(URL)
						if e != nil {
							klog.Errorf("Failed to delete dir err=%v", e)
						}
						if res.StatusCode() != http.StatusOK {
							klog.Infof("delete app cache failed with: %v", res.String())
						}
					}
				}
			} else {
				klog.Error("Failed to get nodes err=%v", e)
			}
		}
	}
	return nil

}

func tryToGetAppdataDirFromDeployment(ctx context.Context, appconfig *appinstaller.ApplicationConfig, owner string) (dirs []string, err error) {
	userspaceNs := utils.UserspaceName(owner)
	config, err := ctrl.GetConfig()
	if err != nil {
		return dirs, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return dirs, err
	}
	sts, err := clientset.AppsV1().StatefulSets(userspaceNs).Get(ctx, "bfl", metav1.GetOptions{})
	if err != nil {
		return dirs, err
	}
	appName := fmt.Sprintf("%s-%s", appconfig.Namespace, appconfig.AppName)
	appCachePath := sts.GetAnnotations()["appcache_hostpath"]
	if len(appCachePath) == 0 {
		return dirs, errors.New("empty appcache_hostpath")
	}
	if !strings.HasSuffix(appCachePath, "/") {
		appCachePath += "/"
	}
	dClient, err := versioned.NewForConfig(config)
	if err != nil {
		return dirs, err
	}
	appCRD, err := dClient.AppV1alpha1().Applications().Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		return dirs, err
	}
	deploymentName := appCRD.Spec.DeploymentName
	deployment, err := clientset.AppsV1().Deployments(appconfig.Namespace).
		Get(context.Background(), deploymentName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return tryToGetAppdataDirFromSts(ctx, appconfig.Namespace, deploymentName, appCachePath)
		}
		return dirs, err
	}

	for _, v := range deployment.Spec.Template.Spec.Volumes {
		if v.HostPath != nil && strings.HasPrefix(v.HostPath.Path, appCachePath) && len(v.HostPath.Path) > len(appCachePath) {
			dirs = append(dirs, filepath.Base(v.HostPath.Path))
		}
	}
	return dirs, nil
}

func tryToGetAppdataDirFromSts(ctx context.Context, namespace, stsName, baseDir string) (dirs []string, err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return dirs, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return dirs, err
	}

	sts, err := clientset.AppsV1().StatefulSets(namespace).
		Get(ctx, stsName, metav1.GetOptions{})
	if err != nil {
		return dirs, err
	}
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.HostPath != nil && strings.HasPrefix(v.HostPath.Path, baseDir) && len(v.HostPath.Path) > len(baseDir) {
			dirs = append(dirs, filepath.Base(v.HostPath.Path))
		}
	}
	return dirs, nil
}

func (r *ApplicationManagerController) upgrade(ctx context.Context, appMgr *appv1alpha1.ApplicationManager) (err error) {
	var version string
	var revision int
	var actionConfig *action.Configuration
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	actionConfig, _, err = helm.InitConfig(config, appMgr.Spec.AppNamespace)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, curRevision, _ := utils.GetDeployedReleaseVersion(actionConfig, appMgr.Spec.AppName)
			klog.Infof("Deployed release curRevision=%d revision=%d", curRevision, revision)
			if curRevision > revision {
				r.rollback(appMgr)
			}

			utils.UpdateAppState(appMgr, appv1alpha1.AppRunning)

			now := metav1.Now()
			message := fmt.Sprintf(constants.OperationFailedTpl, appMgr.Status.OpType, err.Error())
			opRecord := appv1alpha1.OpRecord{
				OpType:     appv1alpha1.UpgradeOp,
				Message:    message,
				Source:     appMgr.Spec.Source,
				Version:    version,
				Status:     appv1alpha1.Failed,
				StatusTime: &now,
			}
			e := r.updateStatus(appMgr, appv1alpha1.Failed, &opRecord, "", message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
			}
		}

	}()

	err = r.updateStatus(appMgr, appv1alpha1.Upgrading, nil, appv1alpha1.AppUpgrading, appv1alpha1.Upgrading.String())

	if err != nil {
		return err
	}
	var appconfig *appinstaller.ApplicationConfig

	deployedVersion, revision, err := utils.GetDeployedReleaseVersion(actionConfig, appMgr.Spec.AppName)
	if err != nil {
		klog.Errorf("Failed to get release revision err=%v", err)
	}

	if !utils.MatchVersion(version, ">= "+deployedVersion) {
		return errors.New("upgrade version should great than deployed version")
	}

	version = appMgr.Status.Payload["version"]
	cfgURL := appMgr.Status.Payload["cfgURL"]
	repoURL := appMgr.Status.Payload["repoURL"]
	token := appMgr.Status.Payload["token"]
	var chartPath string

	if !userspace.IsSysApp(appMgr.Spec.AppName) {
		appconfig, chartPath, err = apiserver.GetAppConfig(ctx, appMgr.Spec.AppName, appMgr.Spec.AppOwner, cfgURL, repoURL, version, token)
		if err != nil {
			return err
		}
		//_, err = apiserver.CheckDependencies(ctx, appconfig.Dependencies, r.Client, appMgr.Spec.AppOwner, false)
		//if err != nil {
		//	return err
		//}
	} else {
		chartPath, err = apiserver.GetIndexAndDownloadChart(ctx, appMgr.Spec.AppName, repoURL, version, token)
		if err != nil {
			return err
		}
		appconfig = &appinstaller.ApplicationConfig{
			AppName:    appMgr.Spec.AppName,
			Namespace:  appMgr.Spec.AppNamespace,
			OwnerName:  appMgr.Spec.AppOwner,
			ChartsName: chartPath,
			RepoURL:    repoURL,
		}
	}

	ops, err := appinstaller.NewHelmOps(ctx, config, appconfig, token, appinstaller.Opt{Source: appMgr.Spec.Source})
	if err != nil {
		return err
	}

	refs, err := utils.GetRefFromResourceList(chartPath)
	if err != nil {
		return err
	}
	err = r.createImageManager(ctx, appMgr, refs)
	if err != nil {
		return err
	}

	err = r.pollDownloadProgress(ctx, appMgr)
	if err != nil {
		return err
	}

	err = ops.Upgrade()

	if err != nil {
		return err
	}

	now := metav1.Now()
	message := fmt.Sprintf(constants.UpgradeOperationCompletedTpl, appMgr.Spec.Type.String(), appMgr.Spec.AppName)
	opRecord := appv1alpha1.OpRecord{
		OpType:     appv1alpha1.UpgradeOp,
		Message:    message,
		Source:     appMgr.Spec.Source,
		Version:    version,
		Status:     appv1alpha1.Completed,
		StatusTime: &now,
	}
	e := r.updateStatus(appMgr, appv1alpha1.Completed, &opRecord, appv1alpha1.AppRunning, message)
	if e != nil {
		klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
	}
	return nil
}

func (r *ApplicationManagerController) rollback(appMgr *appv1alpha1.ApplicationManager) (err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	appconfig := &appinstaller.ApplicationConfig{
		AppName:   appMgr.Spec.AppName,
		Namespace: appMgr.Spec.AppNamespace,
		OwnerName: appMgr.Spec.AppOwner,
	}
	token := appMgr.Status.Payload["token"]
	ops, err := appinstaller.NewHelmOps(context.TODO(), config, appconfig, token, appinstaller.Opt{})
	if err != nil {
		return err
	}

	err = ops.RollBack()

	return err
}

func (r *ApplicationManagerController) cancel(appMgr *appv1alpha1.ApplicationManager) (err error) {
	cancel, ok := manager[appMgr.Name]
	if !ok {
		return errors.New("can not execute cancel")
	}
	cancel()
	var im appv1alpha1.ImageManager
	err = r.Get(context.TODO(), types.NamespacedName{Name: appMgr.Name}, &im)
	if err != nil {
		return err
	}
	imCopy := im.DeepCopy()
	imCopy.Status.State = "canceled"
	now := metav1.Now()
	imCopy.Status.StatusTime = &now
	imCopy.Status.UpdateTime = &now
	err = r.Status().Patch(context.TODO(), imCopy, client.MergeFrom(&im))
	if err != nil {
		return err
	}
	state := appv1alpha1.Canceling
	if appMgr.Status.State == appv1alpha1.Downloading {
		state = appv1alpha1.Canceled
	}

	return r.updateStatus(appMgr, state, nil, "", appMgr.Status.Message)
}

func (r *ApplicationManagerController) suspend(appMgr *appv1alpha1.ApplicationManager, namespace string) (err error) {
	defer func() {
		if err != nil {
			message := fmt.Sprintf(constants.OperationFailedTpl, appMgr.Status.OpType, err.Error())
			e := r.updateStatus(appMgr, appv1alpha1.Failed, nil, "", message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
			}
		}
	}()
	err = suspendOrResumeApp(context.TODO(), r.Client, appMgr, int32(0))
	if err != nil {
		return err
	}
	message := fmt.Sprintf(constants.SuspendOperationCompletedTpl, appMgr.Spec.AppName)
	return r.updateStatus(appMgr, appv1alpha1.Completed, nil, appv1alpha1.AppSuspend, message)
}

func suspendOrResumeApp(ctx context.Context, cli client.Client, appMgr *appv1alpha1.ApplicationManager, replicas int32) error {
	suspend := func(list client.ObjectList) error {
		namespace := appMgr.Spec.AppNamespace
		err := cli.List(ctx, list, client.InNamespace(namespace))
		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get workload namespace=%s err=%v", namespace, err)
			return err
		}

		listObjects, err := apimeta.ExtractList(list)
		if err != nil {
			klog.Errorf("Failed to extract list namespace=%s err=%v", namespace, err)
			return err
		}
		check := func(appName, deployName string) bool {
			if namespace == fmt.Sprintf("user-space-%s", appMgr.Spec.AppOwner) ||
				namespace == fmt.Sprintf("user-system-%s", appMgr.Spec.AppOwner) ||
				namespace == "os-system" {
				if appName == deployName {
					return true
				}
			} else {
				return true
			}
			return false
		}

		//var zeroReplica int32 = 0
		for _, w := range listObjects {
			workloadName := ""
			switch workload := w.(type) {
			case *appsv1.Deployment:
				if check(appMgr.Spec.AppName, workload.Name) {
					workload.Annotations[suspendAnnotation] = "app-service"
					workload.Annotations[suspendCauseAnnotation] = "user operate"
					workload.Spec.Replicas = &replicas
					workloadName = workload.Namespace + "/" + workload.Name
				}
			case *appsv1.StatefulSet:
				if check(appMgr.Spec.AppName, workload.Name) {
					workload.Annotations[suspendAnnotation] = "app-service"
					workload.Annotations[suspendCauseAnnotation] = "user operate"
					workload.Spec.Replicas = &replicas
					workloadName = workload.Namespace + "/" + workload.Name
				}
			}
			if replicas == 0 {
				klog.Infof("Try to suspend workload name=%s", workloadName)
			} else {
				klog.Infof("Try to resume workload name=%s", workloadName)
			}
			err := cli.Update(ctx, w.(client.Object))
			if err != nil {
				klog.Error("Failed to scale workload name=%s err=%v", workloadName, err)
				return err
			}

			klog.Infof("Success to operate workload name=%s", workloadName)
		} // end list object loop

		return nil
	} // end of suspend func

	var deploymentList appsv1.DeploymentList
	err := suspend(&deploymentList)
	if err != nil {
		return err
	}

	var stsList appsv1.StatefulSetList
	err = suspend(&stsList)

	return err
}

func (r *ApplicationManagerController) resumeAppAndWaitForLaunch(appMgr *appv1alpha1.ApplicationManager) (err error) {
	defer func() {
		if err != nil {
			message := fmt.Sprintf(constants.OperationFailedTpl, appMgr.Status.OpType, err.Error())
			e := r.updateStatus(appMgr, appv1alpha1.Failed, nil, "", message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, e)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err = suspendOrResumeApp(ctx, r.Client, appMgr, int32(1))
	if err != nil {
		return err
	}
	err = r.updateStatus(appMgr, appv1alpha1.Resuming, nil, appv1alpha1.AppResuming, appv1alpha1.AppResuming.String())
	if err != nil {
		klog.Errorf("Failed to update applicationmanagers status name=%s err=%v", appMgr.Name, err)
		return err
	}

	var apps appv1alpha1.ApplicationList
	err = r.List(ctx, &apps)
	if err != nil {
		return err
	}
	var app appv1alpha1.Application
	listObjects, err := apimeta.ExtractList(&apps)

	for _, obj := range listObjects {
		a, _ := obj.(*appv1alpha1.Application)
		if a.Spec.Namespace == appMgr.Spec.AppNamespace {
			app = *a
		}
	}
	timer := time.NewTicker(2 * time.Second)
	entrances := app.Spec.Entrances
	entranceCount := len(entrances)
	for {
		select {
		case <-timer.C:
			count := 0
			for _, e := range entrances {
				klog.Infof("Waiting for service launch host=%s", e.Host)
				host := fmt.Sprintf("%s.%s", e.Host, app.Spec.Namespace)
				if utils.TryConnect(host, strconv.Itoa(int(e.Port))) {
					count++
				}
			}
			if entranceCount == count {
				message := fmt.Sprintf(constants.ResumeOperationCompletedTpl, appMgr.Spec.AppName)
				e := r.updateStatus(appMgr, appv1alpha1.Completed, nil, appv1alpha1.AppRunning, message)
				return e
			}

		case <-ctx.Done():
			err = errors.New("wait for resume app to running failed: context canceled")
			return err
		}
	}
}

func (r *ApplicationManagerController) pollDownloadProgress(ctx context.Context, appMgr *appv1alpha1.ApplicationManager) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var im appv1alpha1.ImageManager
			err := r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &im)
			if err != nil {
				klog.Infof("Failed to get imanagermanagers name=%s err=%v", appMgr.Name, err)
				return err
			}

			if im.Status.State == appv1alpha1.Failed.String() {
				return errors.New(im.Status.Message)
			}

			type progress struct {
				offset int64
				total  int64
			}
			maxImageSize := int64(50173680066)

			nodeMap := make(map[string]*progress)
			for _, nodeName := range im.Spec.Nodes {
				for _, ref := range im.Spec.Refs {
					t := im.Status.Conditions[nodeName][ref.Name]["total"]
					if t == "" {
						t = strconv.FormatInt(maxImageSize, 10)
					}

					total, _ := strconv.ParseInt(t, 10, 64)

					t = im.Status.Conditions[nodeName][ref.Name]["offset"]
					if t == "" {
						t = "0"
					}
					offset, _ := strconv.ParseInt(t, 10, 64)

					if _, ok := nodeMap[nodeName]; ok {
						nodeMap[nodeName].offset += offset
						nodeMap[nodeName].total += total
					} else {
						nodeMap[nodeName] = &progress{offset: offset, total: total}
					}
				}
			}
			ret := math.MaxFloat64
			for _, p := range nodeMap {
				var nodeProgress float64
				if p.total != 0 {
					nodeProgress = float64(p.offset) / float64(p.total)
				}
				if nodeProgress < ret {
					ret = nodeProgress * 100
				}

			}

			var cur appv1alpha1.ApplicationManager
			err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &cur)
			if err != nil {
				klog.Infof("Failed to get applicationmanagers name=%v, err=%v", appMgr.Name, err)
				continue
			}

			appMgrCopy := cur.DeepCopy()
			oldProgress, _ := strconv.ParseFloat(cur.Status.Progress, 64)
			if oldProgress > ret {
				continue
			}
			cur.Status.Progress = strconv.FormatFloat(ret, 'f', 2, 64)
			err = r.Status().Patch(ctx, &cur, client.MergeFrom(appMgrCopy))
			if err != nil {
				klog.Infof("Failed to patch applicationmanagers name=%v, err=%v", appMgr.Name, err)
				continue
			}

			klog.Infof("download progress.... %v", cur.Status.Progress)
			if cur.Status.Progress == "100.00" {
				return nil
			}
		case <-ctx.Done():
			return context.Canceled
		}
	}
}

func (r *ApplicationManagerController) createImageManager(ctx context.Context, appMgr *appv1alpha1.ApplicationManager, refs []appv1alpha1.Ref) error {
	var nodes corev1.NodeList
	err := r.List(ctx, &nodes, &client.ListOptions{})
	if err != nil {
		klog.Infof("Failed to list err=%v", err)
		return err
	}

	nodeList := make([]string, 0)
	for _, node := range nodes.Items {
		if !utils.IsNodeReady(&node) || node.Spec.Unschedulable {
			continue
		}
		nodeList = append(nodeList, node.Name)
	}
	if len(nodeList) == 0 {
		return errors.New("cluster has no suitable node to schedule")
	}

	var im appv1alpha1.ImageManager
	err = r.Get(ctx, types.NamespacedName{Name: appMgr.Name}, &im)
	if err == nil {
		err = r.Delete(ctx, &im)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	labels := make(map[string]string)
	if strings.HasSuffix(appMgr.Spec.AppName, "-dev") {
		labels["dev.bytetrade.io/dev-owner"] = appMgr.Spec.AppOwner
	}
	m := appv1alpha1.ImageManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:   appMgr.Name,
			Labels: labels,
		},
		Spec: appv1alpha1.ImageManagerSpec{
			AppName:      appMgr.Spec.AppName,
			AppNamespace: appMgr.Spec.AppNamespace,
			AppOwner:     appMgr.Spec.AppOwner,
			Refs:         refs,
			Nodes:        nodeList,
		},
	}
	err = r.Create(ctx, &m)
	if err != nil {
		return err
	}
	return nil
}

func (r *ApplicationManagerController) updateImStatus(ctx context.Context, name, state, message string) error {
	var im appv1alpha1.ImageManager
	err := r.Get(ctx, types.NamespacedName{Name: name}, &im)
	if err != nil {
		klog.Infof("get im err=%v", err)
		return err
	}

	now := metav1.Now()
	imCopy := im.DeepCopy()
	imCopy.Status.State = state
	imCopy.Status.Message = message
	imCopy.Status.StatusTime = &now
	imCopy.Status.UpdateTime = &now

	err = r.Status().Patch(ctx, imCopy, client.MergeFrom(&im))
	if err != nil {
		return err
	}
	return nil
}

type portKey struct {
	port     int32
	protocol string
}

func setExposePorts(appConfig *appinstaller.ApplicationConfig) error {
	existPorts := make(map[portKey]struct{})
	client, err := utils.GetClient()
	if err != nil {
		return err
	}
	apps, err := client.AppV1alpha1().Applications().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, app := range apps.Items {
		for _, p := range app.Spec.Ports {
			key := portKey{
				port:     p.ExposePort,
				protocol: string(p.Protocol),
			}
			existPorts[key] = struct{}{}
		}
	}
	klog.Infof("existPorts: %v", existPorts)

	for i := range appConfig.Ports {
		port := &appConfig.Ports[i]
		if port.ExposePort == 0 {
			var exposePort int32
			for i := 0; i < 5; i++ {

				exposePort, err = genPort(port.Protocol)
				if err != nil {
					continue
				}
				key := portKey{port: exposePort, protocol: port.Protocol}
				if _, ok := existPorts[key]; !ok && err == nil {
					break
				}
			}
			key := portKey{port: exposePort, protocol: port.Protocol}
			if _, ok := existPorts[key]; ok || err != nil {
				return fmt.Errorf("%d port is not available", key.port)
			}
			existPorts[key] = struct{}{}
			port.ExposePort = exposePort
		}
	}
	return nil
}

func genPort(protocol string) (int32, error) {
	exposePort := int32(rand.IntnRange(33333, 36789))
	if !utils.IsPortAvailable(protocol, int(exposePort)) {
		return 0, fmt.Errorf("failed to allocate an available port after 3 attempts")
	}
	return exposePort, nil
}
