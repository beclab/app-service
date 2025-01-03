package apiserver

import (
	"encoding/json"
	"sort"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO: get owner's app with appname, instead to get by namespace and name
func (h *Handler) get(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	appName := req.PathParameter(ParamAppName)
	appNamespace := req.PathParameter(ParamAppNamespace)

	name := utils.FmtAppName(appName, appNamespace)

	// run with request context for incoming client
	app, err := client.AppClient.AppV1alpha1().Applications().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if apierrors.IsNotFound(err) {
		am, err := client.AppClient.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(), name, metav1.GetOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		var appconfig appinstaller.ApplicationConfig
		err = json.Unmarshal([]byte(am.Spec.Config), &appconfig)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		app = &appv1alpha1.Application{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: metav1.Now(),
			},
			Spec: appv1alpha1.ApplicationSpec{
				Name:      am.Spec.AppName,
				Appid:     utils.GetAppID(am.Spec.AppName),
				IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
				Namespace: am.Spec.AppNamespace,
				Owner:     owner,
				Entrances: appconfig.Entrances,
				Icon:      appconfig.Icon,
			},
		}
	}

	resp.WriteAsJson(app)
}

func (h *Handler) list(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	// run with request context for incoming client
	allApps, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	var filteredApps []appv1alpha1.Application
	for _, a := range allApps.Items {
		if a.Spec.Owner == owner {
			filteredApps = append(filteredApps, a)
		}
	}

	// get pending app's from app managers
	//var pendingApplications []appv1alpha1.Application
	appMgrs, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	for _, am := range appMgrs.Items {
		var appconfig appinstaller.ApplicationConfig
		err = json.Unmarshal([]byte(am.Spec.Config), &appconfig)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		if am.Spec.AppOwner == owner && am.Status.State == appv1alpha1.Pending {
			app := appv1alpha1.Application{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:              am.Name,
					CreationTimestamp: metav1.Now(),
				},
				Spec: appv1alpha1.ApplicationSpec{
					Name:      am.Spec.AppName,
					Appid:     utils.GetAppID(am.Spec.AppName),
					IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
					Namespace: am.Spec.AppNamespace,
					Owner:     owner,
					Entrances: appconfig.Entrances,
					Icon:      appconfig.Icon,
				},
			}
			filteredApps = append(filteredApps, app)
		}
	}

	// sort by create time desc
	sort.Slice(filteredApps, func(i, j int) bool {
		return filteredApps[i].CreationTimestamp.Before(&filteredApps[j].CreationTimestamp)
	})

	resp.WriteAsJson(filteredApps)
}

func (h *Handler) listBackend(req *restful.Request, resp *restful.Response) {
	appClient, err := versioned.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	owner := req.PathParameter(ParamUserName)

	appsMap := make(map[string]appv1alpha1.Application)

	// get downloading, installing state app from app managers
	ams, err := appClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, am := range ams.Items {
		if am.Spec.Type != appv1alpha1.App {
			continue
		}
		if am.Spec.AppOwner == owner && (am.Status.State == appv1alpha1.Pending || am.Status.State == appv1alpha1.Downloading || am.Status.State == appv1alpha1.Installing) {
			var appConfig appinstaller.ApplicationConfig
			err = json.Unmarshal([]byte(am.Spec.Config), &appConfig)
			if err != nil {
				api.HandleError(resp, req, err)
				return
			}
			now := metav1.Now()
			name, _ := utils.FmtAppMgrName(am.Spec.AppName, owner, appConfig.Namespace)
			app := appv1alpha1.Application{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					CreationTimestamp: am.CreationTimestamp,
				},
				Spec: appv1alpha1.ApplicationSpec{
					Name:      am.Spec.AppName,
					Appid:     utils.GetAppID(am.Spec.AppName),
					IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
					Namespace: am.Spec.AppNamespace,
					Owner:     owner,
					Entrances: appConfig.Entrances,
					Icon:      appConfig.Icon,
				},
				Status: appv1alpha1.ApplicationStatus{
					State:      am.Status.State.String(),
					StatusTime: &now,
					UpdateTime: &now,
				},
			}
			appsMap[app.Name] = app
		}
	}

	// run with request context for incoming client
	allApps, err := appClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	var filteredApps []appv1alpha1.Application
	for _, a := range allApps.Items {
		if a.Spec.Owner == owner {
			appsMap[a.Name] = a
		}
	}

	for _, app := range appsMap {
		filteredApps = append(filteredApps, app)
	}

	// sort by create time desc
	sort.Slice(filteredApps, func(i, j int) bool {
		return filteredApps[i].CreationTimestamp.Before(&filteredApps[j].CreationTimestamp)
	})

	resp.WriteAsJson(filteredApps)
}
