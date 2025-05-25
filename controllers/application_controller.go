package controllers

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/thoas/go-funk"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
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

const (
	applicationFinalizer = "finalizers.bytetrade.io/application"
)

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	AppClientset *versioned.Clientset
	Kubeconfig   *rest.Config
}

//+kubebuilder:rbac:groups=app.bytetrade.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.bytetrade.io,resources=applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=app.bytetrade.io,resources=applications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Application object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	ctrl.Log.Info("reconcile request", "name", req.Name, "namespace", req.Namespace)

	if req.Namespace == "" {
		// ignore for-input object watch
		return ctrl.Result{}, nil
	}

	var validAppObject client.Object
	// get deployments installed by app installer
	findAppObject := func(list client.ObjectList) error {
		if err := r.List(ctx, list, client.InNamespace(req.Namespace)); err == nil {
			listObjects, err := apimeta.ExtractList(list)
			if err != nil {
				ctrl.Log.Error(err, "extract list error", "name label", req.Name, "namespace", req.Namespace)
				return err
			}
			for _, o := range listObjects {
				d := o.(client.Object)

				if d.GetDeletionTimestamp() == nil {
					// for multi-app in one deployment/statefulset, we can not find only one object via
					// namespace and label filter, so have to filter in object list
					apps := getAppName(d)
					isValid := true
					for _, name := range strings.Split(req.Name, ",") {
						if !funk.Contains(apps, name) {
							isValid = false
							break
						}
					}
					if !isValid {
						continue
					}
					owner, ok := d.GetLabels()[constants.ApplicationOwnerLabel]
					if validAppObject != nil || !ok || owner == "" {
						// duplicate or ownerless deployment is invalid
						ctrl.Log.Info("delete invalid deployment or statefulset", "name", d.GetName(), "namespace", d.GetNamespace())
						err = r.Delete(ctx, d)
						if err != nil {
							ctrl.Log.Error(err, "delete invalid deployment or statefulset error", "name", d.GetName(), "namespace", d.GetNamespace())
						}
					} else {
						validAppObject = d
						break
					}
				} // end if deployment is deleted
			} // end loop deployment.Items
		} else {
			ctrl.Log.Error(err, "list deployments or statefulset error", "name label", req.Name, "namespace", req.Namespace)
			return err
		} // end if get deployments list

		return nil
	}

	var deployemnts appsv1.DeploymentList
	err := findAppObject(&deployemnts)
	if err != nil {
		return ctrl.Result{}, err
	}

	// try to get statefulset
	if validAppObject == nil {
		var statefulsets appsv1.StatefulSetList
		err := findAppObject(&statefulsets)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	appNames := strings.Split(req.Name, ",")

	for _, name := range appNames {
		app, err := r.AppClientset.AppV1alpha1().Applications().Get(ctx, fmtAppName(name, req.Namespace), metav1.GetOptions{})
		if validAppObject != nil {
			// create or update application
			if err != nil {
				if apierrors.IsNotFound(err) {
					// check if a new deployment created or not
					ctrl.Log.Info("create app from deployment watching", "name", validAppObject.GetName(), "namespace", validAppObject.GetNamespace(), "appname", name)
					err = r.createApplication(ctx, req, validAppObject, name)
					if err != nil {
						ctrl.Log.Info("create app failed", "app", name, "err", err)
						return ctrl.Result{}, err
					}
					continue
				}
				return ctrl.Result{}, err
			} // end if error

			//owner := validAppObject.GetLabels()[constants.ApplicationOwnerLabel]
			//analyticsEnabled := validAppObject.GetAnnotations()[constants.ApplicationAnalytics]
			//if analyticsEnabled == "" {
			//	analyticsEnabled = "false"
			//}
			//actionConfig, _, err := helm.InitConfig(r.Kubeconfig, app.Spec.Namespace)
			//if err != nil {
			//	ctrl.Log.Error(err, "init helm config error")
			//	return ctrl.Result{}, err
			//}
			//versionChanged := false
			//if !userspace.IsSysApp(app.Spec.Name) {
			//	version, _, err := utils.GetDeployedReleaseVersion(actionConfig, name)
			//	if err != nil {
			//		ctrl.Log.Error(err, "get release version error")
			//		return ctrl.Result{}, err
			//	}
			//	if app.Spec.Settings["version"] != version {
			//		versionChanged = true
			//	}
			//}
			//if app.Spec.Namespace != validAppObject.GetNamespace() ||
			//	app.Spec.Name != name ||
			//	app.Spec.Owner != owner ||
			//	app.Spec.DeploymentName != validAppObject.GetName() ||
			//	app.Spec.Settings["analyticsEnabled"] != analyticsEnabled ||
			//	versionChanged {
			ctrl.Log.Info("Application update", "name", app.Name, "spec.name", app.Spec.Name, "spec.owner", app.Spec.Owner)
			err = r.updateApplication(ctx, req, validAppObject, app, name)
			if err != nil {
				return ctrl.Result{Requeue: true}, err
			}
			//}
		} else {
			// deployment or statefulset is nil, delete application
			if err == nil && app != nil {
				//client, _ := kubernetes.NewForConfig(r.Kubeconfig)
				//if utils.IsProtectedNamespace(app.Spec.Namespace) {
				//	_, err = client.CoreV1().Namespaces().Get(context.TODO(), "not exists namespace", metav1.GetOptions{})
				//} else {
				//	_, err = client.CoreV1().Namespaces().Get(context.TODO(), app.Spec.Namespace, metav1.GetOptions{})
				//}

				ctrl.Log.Info("Application delete", "name", app.Name, "spec.name", app.Spec.Name, "spec.owner", app.Spec.Owner)
				err = r.Delete(ctx, app.DeepCopy())
				if err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				err = r.clearHelmHistory(app.Spec.Name, app.Spec.Namespace)
				if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
					return ctrl.Result{RequeueAfter: 2 * time.Second}, err
				}

				//if err != nil {
				//	if apierrors.IsNotFound(err) {
				//		ctrl.Log.Info("Application delete", "name", app.Name, "spec.name", app.Spec.Name, "spec.owner", app.Spec.Owner)
				//		err = r.Delete(ctx, app.DeepCopy())
				//		if err != nil && !apierrors.IsNotFound(err) {
				//			return ctrl.Result{}, err
				//		}
				//
				//		err = r.clearHelmHistory(app.Spec.Name, app.Spec.Namespace)
				//	} else {
				//		// get namespace err, re-enqueue
				//		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
				//	}
				//} else {
				//	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
				//
				//}
			} else if apierrors.IsNotFound(err) {
				// app not found, just return
				return ctrl.Result{}, nil
			}
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.Application{}).
		Build(r)

	if err != nil {
		return err
	}

	// watch the application enqueue formarted request
	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.Application{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				app := h.(*appv1alpha1.Application)
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      app.Spec.Name,
					Namespace: app.Spec.Namespace}}}
			}))
	if err != nil {
		return err
	}

	watches := []client.Object{
		&appsv1.Deployment{},
		&appsv1.StatefulSet{},
	}

	// watch the object installed by app-installer
	for _, w := range watches {
		if err = r.addWatch(c, w); err != nil {
			return err
		}
	}
	return nil
}

func (r *ApplicationReconciler) addWatch(c controller.Controller, watchedObject client.Object) error {
	return c.Watch(
		&source.Kind{Type: watchedObject},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				appNames := getAppName(h)
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      strings.Join(appNames, ","),
					Namespace: h.GetNamespace()}}}
			}),
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return isApp(e.ObjectNew, e.ObjectOld)
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return isApp(e.Object)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return isApp(e.Object)
			},
		})
}

// TODO: get application other spec info
// TODO: make sure entrance service is applied
func (r *ApplicationReconciler) createApplication(ctx context.Context, req ctrl.Request,
	deployment client.Object, name string) error {
	owner := deployment.GetLabels()[constants.ApplicationOwnerLabel]
	appNames := getAppName(deployment)
	isMultiApp := len(appNames) > 1
	icon := getAppIcon(deployment)
	entrancesMap, err := r.getEntranceServiceAddress(ctx, deployment, isMultiApp)
	if err != nil {
		ctrl.Log.Error(err, "get entrance error")
	}
	servicePortsMap, err := r.getAppPorts(ctx, deployment, isMultiApp)
	if err != nil {
		klog.Warningf("get app ports err=%v", err)
	}
	tailScale, err := r.getAppTailScale(deployment)
	if err != nil {
		klog.Warningf("get app tailscale acls err=%v", err)
	}

	var appid string
	var isSysApp bool
	if userspace.IsSysApp(name) {
		appid = name
		isSysApp = true
	} else {
		appid = utils.Md5String(name)[:8]
	}
	settings := r.getAppSettings(ctx, name, appid, owner, deployment, isMultiApp, entrancesMap[name])
	// create the application cr
	newapp := &appv1alpha1.Application{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmtAppName(name, req.Namespace),
		},
		Spec: appv1alpha1.ApplicationSpec{
			Name:           name,
			Appid:          appid,
			IsSysApp:       isSysApp,
			Namespace:      req.Namespace,
			Owner:          owner, // get from deployment
			DeploymentName: deployment.GetName(),
			Entrances:      entrancesMap[name],
			Ports:          servicePortsMap[name],
			Icon:           icon[name],
			Settings:       settings,
		},
	}
	if tailScale != nil {
		newapp.Spec.TailScale = *tailScale
	}
	app, err := r.AppClientset.AppV1alpha1().Applications().Create(ctx, newapp, metav1.CreateOptions{})
	if err != nil {
		ctrl.Log.Error(err, "create application error")
	}
	now := metav1.Now()
	appCopy := app.DeepCopy()
	if userspace.IsSysApp(app.Spec.Name) {
		err = utils.CreateSysAppMgr(app.Spec.Name, app.Spec.Owner)
		if err != nil {
			klog.Errorf("Failed to create applicationmanagers for system app=%s err=%v", app.Spec.Name, err)
		}
	}

	app.Status.StatusTime = &now
	app.Status.UpdateTime = &now
	app.Status.State = appv1alpha1.AppNotReady.String()

	entranceStatues := make([]appv1alpha1.EntranceStatus, 0, len(app.Spec.Entrances))

	for _, e := range app.Spec.Entrances {
		entranceStatues = append(entranceStatues, appv1alpha1.EntranceStatus{
			Name:       e.Name,
			State:      appv1alpha1.EntranceNotReady,
			StatusTime: &now,
			Reason:     appv1alpha1.EntranceNotReady.String(),
		})
	}
	app.Status.EntranceStatuses = entranceStatues

	err = r.Status().Patch(ctx, app, client.MergeFrom(appCopy))
	if err != nil {
		klog.Infof("Failed to patch err=%v", err)
	}

	return err
}

func (r *ApplicationReconciler) updateApplication(ctx context.Context, req ctrl.Request,
	deployment client.Object, app *appv1alpha1.Application, name string) error {
	appCopy := app.DeepCopy()

	tailScale, err := r.getAppTailScale(deployment)
	if err != nil {
		klog.Errorf("failed to get tailscale err=%v", err)
	}

	owner := deployment.GetLabels()[constants.ApplicationOwnerLabel]
	klog.Infof("in updateApplication ....")
	icons := getAppIcon(deployment)
	var icon string

	icon = icons[name]

	appCopy.Spec.Name = name
	appCopy.Spec.Namespace = deployment.GetNamespace()
	appCopy.Spec.Owner = owner
	appCopy.Spec.DeploymentName = deployment.GetName()
	appCopy.Spec.Icon = icon
	if tailScale != nil {
		appCopy.Spec.TailScale = *tailScale
	}

	actionConfig, _, err := helm.InitConfig(r.Kubeconfig, appCopy.Spec.Namespace)
	if err != nil {
		ctrl.Log.Error(err, "init helm config error")
	}

	if !userspace.IsSysApp(app.Spec.Name) {
		version, _, err := utils.GetDeployedReleaseVersion(actionConfig, name)
		if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
			ctrl.Log.Error(err, "get deployed release version error")
		}
		if err == nil {
			appCopy.Spec.Settings["version"] = version
		}
	}

	err = r.Patch(ctx, appCopy, client.MergeFrom(app))
	if err != nil {
		klog.Infof("update spec failed %v", err)
		return err
	}

	klog.Infof("appCopy.Status: %v", appCopy.Status)
	newAppState := r.calAppState(&appCopy.Status)
	klog.Infof("application controller newAppState: %v", newAppState)
	klog.Infof("application controller oldAppState: %v", appCopy.Status.State)

	if appCopy.Status.State != newAppState {
		klog.Infof("set appCopy.State:.......new: %v", newAppState)
		appCopy.Status.State = newAppState
		now := metav1.Now()
		appCopy.Status.LastTransitionTime = &now

		err = r.Status().Patch(ctx, appCopy, client.MergeFrom(app))
		if err != nil {
			klog.Infof("update xxx error: %v", err)
			return err
		}
	}

	// merge settings
	//for k, v := range settings {
	//	if setting, ok := appCopy.Spec.Settings[k]; !ok || setting != v {
	//		appCopy.Spec.Settings[k] = v
	//	}
	//}

	//var a appv1alpha1.Application
	//err = r.Get(ctx, types.NamespacedName{Name: app.Name}, &a)
	//if err != nil {
	//	klog.Infof("get app failed %v", err)
	//	return err
	//}
	//klog.Infof("appState: ..%v", a.Status.State)
	return err
}

func (r *ApplicationReconciler) getEntranceServiceAddress(ctx context.Context, deployment client.Object, isMultiApp bool) (map[string][]appv1alpha1.Entrance, error) {
	entrancesLabel := deployment.GetAnnotations()[constants.ApplicationEntrancesKey]
	entrancesMap := make(map[string][]appv1alpha1.Entrance)

	if len(entrancesLabel) == 0 {
		return entrancesMap, errors.New("invalid service address label")
	}
	klog.Infof("isMultiApp: %v", isMultiApp)
	var err error
	if isMultiApp {
		err = json.Unmarshal([]byte(entrancesLabel), &entrancesMap)
		if err != nil {
			klog.Infof("unmarshalMAp error=%v", err)
			return nil, err
		}
	} else {
		appName := deployment.GetLabels()[constants.ApplicationNameLabel]
		entrances := make([]appv1alpha1.Entrance, 0)
		err = json.Unmarshal([]byte(entrancesLabel), &entrances)
		if err != nil {
			klog.Infof("unmarshal error=%v", err)
			return nil, err
		}
		entrancesMap[appName] = entrances
	}

	// set default value and check if service exists
	for _, entrances := range entrancesMap {
		for i, e := range entrances {
			if e.AuthLevel == "" {
				entrances[i].AuthLevel = constants.AuthorizationLevelOfPrivate
			}
			if e.OpenMethod == "" {
				entrances[i].OpenMethod = "default"
			}
			objectKey := types.NamespacedName{Namespace: deployment.GetNamespace(), Name: e.Host}
			var svc corev1.Service
			if err = r.Get(ctx, objectKey, &svc); err == nil {
				if !checkPortOfService(&svc, e.Port) {
					return nil, fmt.Errorf("entrance: %s not found", e.Host)
				}
			} else {
				return nil, err
			}
		}
	}
	return entrancesMap, nil
}

func (r *ApplicationReconciler) getAppSettings(ctx context.Context, appName, appId, owner string, deployment client.Object,
	isMulti bool, entrances []appv1alpha1.Entrance) map[string]string {
	settings := make(map[string]string)
	settings["source"] = api.Unknown.String()

	if chartSource, ok := deployment.GetAnnotations()[constants.ApplicationSourceLabel]; ok {
		settings["source"] = chartSource
	}

	if systemService, ok := deployment.GetLabels()[constants.ApplicationSystemServiceLabel]; ok {
		settings["system_service"] = systemService
	}

	titles := getAppTitle(deployment)
	settings["title"] = titles[appName]

	if target, ok := deployment.GetLabels()[constants.ApplicationTargetLabel]; ok {
		settings["target"] = target
	}

	versions := getAppVersion(deployment)
	settings["version"] = versions[appName]

	settings["analyticsEnabled"] = "false"
	analyticsEnabledFromAnnotation, ok := deployment.GetAnnotations()[constants.ApplicationAnalytics]
	if ok && analyticsEnabledFromAnnotation == "true" {
		settings["analyticsEnabled"] = "true"
	}

	settings["clusterScoped"] = "false"
	//clusterScoped, ok := deployment.GetAnnotations()[constants.ApplicationClusterScoped]
	//if ok && clusterScoped == "true" {
	//	settings["clusterScoped"] = "true"
	//}

	// not sys applications.
	if !userspace.IsSysApp(appName) {
		if appCfg, err := appinstaller.GetAppInstallationConfig(appName, owner); err != nil {
			klog.Infof("Failed to get app configuration appName=%s owner=%s err=%v", appName, owner, err)
		} else {
			policyStr, err := getApplicationPolicy(appCfg.Policies, appCfg.Entrances)
			if err != nil {
				klog.Errorf("Failed to encode json err=%v", err)
			} else if len(policyStr) > 0 {
				settings[applicationSettingsPolicyKey] = policyStr
			}

			if appCfg.AnalyticsEnabled {
				settings["analyticsEnabled"] = "true"
			}

			// set cluster-scoped info to settings
			if appCfg.AppScope.ClusterScoped {
				settings["clusterScoped"] = "true"
				if len(appCfg.AppScope.AppRef) > 0 {
					settings["clusterAppRef"] = strings.Join(appCfg.AppScope.AppRef, ",")
				}
			}
			if appCfg.MobileSupported {
				settings["mobileSupported"] = "true"
			} else {
				settings["mobileSupported"] = "false"
			}

			if appCfg.OIDC.Enabled {
				// get oidc client id and secret created at installing
				var secret corev1.Secret
				err = r.Get(ctx,
					types.NamespacedName{Namespace: deployment.GetNamespace(), Name: constants.OIDCSecret},
					&secret)
				if err != nil {
					klog.Errorf("Failed to get app's oidc secret err=%v, app=%s, namespace=%s", err, appName, deployment.GetNamespace())
				} else {
					settings["oidc.client.id"] = string(secret.Data["id"])

					encryptSecret, err := utils.Pbkdf2Crypto(string(secret.Data["secret"]))
					if err != nil {
						klog.Error("encrypt secret error, ", err)
					}
					settings["oidc.client.secret"] = encryptSecret

					zone, err := kubesphere.GetUserZone(ctx, r.Kubeconfig, owner)
					if err != nil {
						klog.Error("get user zone error, ", err)
					} else {

						multiEntrance := len(appCfg.Entrances) > 1
						for i, e := range appCfg.Entrances {
							if e.Name == appCfg.OIDC.EntranceName {
								var appUrl string
								if multiEntrance {
									appUrl = fmt.Sprintf("https://%s%d.%s%s", appId, i, zone, appCfg.OIDC.RedirectUri)
								} else {
									appUrl = fmt.Sprintf("https://%s.%s%s", appId, zone, appCfg.OIDC.RedirectUri)
								}
								settings["oidc.client.redirect_uri"] = appUrl
							}
						}

					} // end of if get zone
				} // end of if get secret
			}
		}
	} else {
		// sys applications.
		type Policies struct {
			Policies []appcfg.Policy `json:"policies"`
		}
		applicationPoliciesFromAnnotation, ok := deployment.GetAnnotations()[constants.ApplicationPolicies]

		var policy Policies
		if ok {
			if isMulti {
				m := make(map[string]Policies)
				err := json.Unmarshal([]byte(applicationPoliciesFromAnnotation), &m)
				if err != nil {
					klog.Errorf("Failed to unmarshal applicationPoliciesFromAnnotation err=%v", err)
				}
				policy = m[appName]
			} else {
				err := json.Unmarshal([]byte(applicationPoliciesFromAnnotation), &policy)
				if err != nil {
					klog.Errorf("Failed to unmarshal applicationPoliciesFromAnnotation err=%v", err)
				}
			}
		}

		// transform from Policy to AppPolicy
		var appPolicies []appcfg.AppPolicy
		for _, p := range policy.Policies {
			d, _ := time.ParseDuration(p.Duration)
			appPolicies = append(appPolicies, appcfg.AppPolicy{
				EntranceName: p.EntranceName,
				URIRegex:     p.URIRegex,
				Level:        p.Level,
				OneTime:      p.OneTime,
				Duration:     d,
			})
		}
		policyStr, err := getApplicationPolicy(appPolicies, entrances)
		if err != nil {
			klog.Errorf("Failed to encode json err=%v", err)
		} else if len(policyStr) > 0 {
			settings[applicationSettingsPolicyKey] = policyStr
		}
		settings["source"] = api.System.String()
		mobileSupported, ok := deployment.GetAnnotations()[constants.ApplicationMobileSupported]
		settings["mobileSupported"] = "false"
		if ok {
			settings["mobileSupported"] = mobileSupported
		}
	}

	return settings
}

func (r *ApplicationReconciler) clearHelmHistory(appname, namespace string) error {
	actionConfig, _, err := helm.InitConfig(r.Kubeconfig, namespace)
	if err != nil {
		return err
	}
	klog.Infof("clearHelmHistory: appname:%s, namespace:%s", appname, namespace)

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	_, err = histClient.Run(appname)
	klog.Infof("appname in clearHelmHistory: %v", appname)
	klog.Infof("err in clearHelmHistory: err=%v", err)

	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil
		}
		return err
	}

	return helm.UninstallCharts(actionConfig, appname)
}

func (r *ApplicationReconciler) getAppPorts(ctx context.Context, deployment client.Object, isMultiApp bool) (map[string][]appv1alpha1.ServicePort, error) {
	portsLabel := deployment.GetAnnotations()[constants.ApplicationPortsKey]
	portsMap := make(map[string][]appv1alpha1.ServicePort)
	if len(portsLabel) == 0 {
		return portsMap, errors.New("invalid service port")
	}
	var err error
	if isMultiApp {
		err = json.Unmarshal([]byte(portsLabel), &portsMap)
		if err != nil {
			klog.Errorf("unmarshal portMap err=%v", err)
			return nil, err
		}
	} else {
		appName := deployment.GetLabels()[constants.ApplicationNameLabel]
		ports := make([]appv1alpha1.ServicePort, 0)
		err = json.Unmarshal([]byte(portsLabel), &ports)
		if err != nil {
			klog.Errorf("unmarshal service port error=%v", err)
			return nil, err
		}
		portsMap[appName] = ports
	}
	return portsMap, nil
}

func (r *ApplicationReconciler) getAppTailScale(deployment client.Object) (*appv1alpha1.TailScale, error) {
	tailScale := appv1alpha1.TailScale{}
	tailScaleString := deployment.GetAnnotations()[constants.ApplicationTailScaleKey]
	err := json.Unmarshal([]byte(tailScaleString), &tailScale)
	if err != nil {
		return nil, err
	}
	return &tailScale, nil
}

func (r *ApplicationReconciler) calAppState(status *appv1alpha1.ApplicationStatus) string {
	entranceLen := len(status.EntranceStatuses)
	klog.Infof("entranceLen: %v", entranceLen)
	if entranceLen == 0 {
		return "running"
	}
	for _, es := range status.EntranceStatuses {
		if es.State == appv1alpha1.EntranceStopped {
			return "stopped"
		}
		if es.State == appv1alpha1.EntranceNotReady {
			return "notReady"
		}
	}
	return "running"
}

func checkPortOfService(s *corev1.Service, port int32) bool {
	for _, p := range s.Spec.Ports {
		if p.Port == port {
			return true
		}
	}

	return false
}

func fmtAppName(name, namespace string) string {
	return appv1alpha1.AppResourceName(name, namespace)
}

func isApp(obs ...metav1.Object) bool {
	for _, o := range obs {

		if o.GetLabels() == nil {
			return false
		}

		if _, ok := o.GetLabels()[constants.ApplicationNameLabel]; !ok {
			return false
		}
	}
	return true
}

func isWorkflow(obs ...metav1.Object) bool {
	for _, o := range obs {

		if o.GetLabels() == nil {
			return false
		}

		if _, ok := o.GetLabels()[constants.WorkflowNameLabel]; !ok {
			return false
		}
	}
	return true
}

func getApplicationPolicy(policies []appcfg.AppPolicy, entrances []appv1alpha1.Entrance) (string, error) {
	subPolicy := make(map[string][]*applicationSettingsSubPolicy)

	for _, p := range policies {
		subPolicy[p.EntranceName] = append(subPolicy[p.EntranceName],
			&applicationSettingsSubPolicy{
				URI:      p.URIRegex,
				Policy:   p.Level,
				OneTime:  p.OneTime,
				Duration: int32(p.Duration / time.Second),
			})
	}

	policy := make(map[string]applicationSettingsPolicy)
	for _, e := range entrances {
		defaultPolicy := "system"
		sp := subPolicy[e.Name]
		if e.AuthLevel == constants.AuthorizationLevelOfPublic {
			defaultPolicy = constants.AuthorizationLevelOfPublic
		}
		policy[e.Name] = applicationSettingsPolicy{
			DefaultPolicy: defaultPolicy,
			OneTime:       false,
			Duration:      0,
			SubPolicies:   sp,
		}
	}

	policyStr, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(policyStr), nil
}

func getEntranceFromAnnotations(deployment client.Object) ([]appv1alpha1.Entrance, error) {
	entrancesLabel := deployment.GetAnnotations()[constants.ApplicationEntrancesKey]
	entrances := make([]appv1alpha1.Entrance, 0)

	if len(entrancesLabel) == 0 {
		return entrances, errors.New("invalid service address label")
	}

	if err := json.Unmarshal([]byte(entrancesLabel), &entrances); err != nil {
		return entrances, err
	}
	for i, e := range entrances {
		if e.OpenMethod == "" {
			entrances[i].OpenMethod = "default"
		}
	}

	return entrances, nil
}

func getAppName(deployment client.Object) []string {
	names := make([]string, 0)
	isMultiApp := deployment.GetLabels()[constants.ApplicationAppGroupLabel] == "true"
	if isMultiApp {
		apps := make(map[string]interface{})
		_ = json.Unmarshal([]byte(deployment.GetAnnotations()[constants.ApplicationEntrancesKey]), &apps)
		for k := range apps {
			names = append(names, k)
		}
		return names
	}
	name := deployment.GetLabels()[constants.ApplicationNameLabel]
	return []string{name}
}

func getAppIcon(deployment client.Object) map[string]string {
	ret := make(map[string]string)
	if deployment.GetLabels()[constants.ApplicationAppGroupLabel] == "true" {
		err := json.Unmarshal([]byte(deployment.GetAnnotations()[constants.ApplicationIconLabel]), &ret)
		if err != nil {
			klog.Infof("Failed to unmarshal application icon label err=%v", err)
		}
	} else {
		ret[deployment.GetLabels()[constants.ApplicationNameLabel]] = deployment.GetAnnotations()[constants.ApplicationIconLabel]
	}
	return ret
}

func getAppVersion(deployment client.Object) map[string]string {
	ret := make(map[string]string)
	if deployment.GetLabels()[constants.ApplicationAppGroupLabel] == "true" {
		err := json.Unmarshal([]byte(deployment.GetAnnotations()[constants.ApplicationVersionLabel]), &ret)
		if err != nil {
			klog.Infof("Failed to unmarshal application icon label err=%v", err)
		}
	} else {
		ret[deployment.GetLabels()[constants.ApplicationNameLabel]] = deployment.GetAnnotations()[constants.ApplicationVersionLabel]
	}
	return ret
}

func getAppTitle(deployment client.Object) map[string]string {
	ret := make(map[string]string)
	if deployment.GetLabels()[constants.ApplicationAppGroupLabel] == "true" {
		err := json.Unmarshal([]byte(deployment.GetAnnotations()[constants.ApplicationTitleLabel]), &ret)
		if err != nil {
			klog.Infof("Failed to unmarshal application icon label err=%v", err)
		}
	} else {
		ret[deployment.GetLabels()[constants.ApplicationNameLabel]] = deployment.GetAnnotations()[constants.ApplicationTitleLabel]
	}
	return ret
}
