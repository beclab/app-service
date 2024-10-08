package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/upgrade"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *Handler) status(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var a v1alpha1.Application
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	key := types.NamespacedName{Name: name}
	err = h.ctrlClient.Get(req.Request.Context(), key, &a)
	if err != nil && !apierrors.IsNotFound(err) {
		api.HandleError(resp, req, err)
		return
	}
	source := a.Spec.Settings["source"]
	if source == "" {
		source = api.Unknown.String()
	}
	sts := appinstaller.Status{
		Name:              a.Spec.Name,
		AppID:             a.Spec.Appid,
		Namespace:         a.Spec.Namespace,
		CreationTimestamp: a.CreationTimestamp,
		Source:            source,
		AppStatus:         a.Status,
	}
	if apierrors.IsNotFound(err) {
		var am v1alpha1.ApplicationManager
		e := h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)
		if e != nil {
			api.HandleError(resp, req, e)
			return
		}

		if (am.Status.OpType == v1alpha1.InstallOp && am.Status.State == v1alpha1.Canceled) ||
			(am.Status.OpType == v1alpha1.UninstallOp && am.Status.State == v1alpha1.Completed) {
			api.HandleNotFound(resp, req, fmt.Errorf("app %s not found", app))
			return
		}

		state := v1alpha1.Pending
		if am.Status.State == v1alpha1.Downloading || am.Status.State == v1alpha1.Installing {
			state = am.Status.State
		}

		now := metav1.Now()
		sts = appinstaller.Status{
			Name:              am.Spec.AppName,
			AppID:             utils.GetAppID(am.Spec.AppName),
			Namespace:         am.Spec.AppNamespace,
			CreationTimestamp: now,
			Source:            am.Spec.Source,
			AppStatus: v1alpha1.ApplicationStatus{
				State:      state.String(),
				StatusTime: &now,
				UpdateTime: &now,
			},
		}
	}

	resp.WriteAsJson(sts)
}

func (h *Handler) appsStatus(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	isSysApp := req.QueryParameter("issysapp")
	state := req.QueryParameter("state")
	ss := make([]string, 0)
	if state != "" {
		ss = strings.Split(state, "|")
	}
	stateSet := constants.States
	if len(ss) > 0 {
		stateSet = sets.String{}
	}
	for _, s := range ss {
		stateSet.Insert(s)
	}

	// run with request context for incoming client
	allApps, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	filteredApps := make([]appinstaller.Status, 0)
	appSets := sets.String{}
	for _, a := range allApps.Items {
		if a.Spec.Owner == owner {
			if !stateSet.Has(a.Status.State) {
				continue
			}
			if len(isSysApp) > 0 && strconv.FormatBool(a.Spec.IsSysApp) != isSysApp {
				continue
			}
			source := a.Spec.Settings["source"]
			if source == "" {
				source = api.Unknown.String()
			}
			status := appinstaller.Status{
				Name:              a.Spec.Name,
				AppID:             a.Spec.Appid,
				Namespace:         a.Spec.Namespace,
				CreationTimestamp: a.CreationTimestamp,
				Source:            source,
				AppStatus:         a.Status,
			}
			appSets.Insert(a.Spec.Name)
			filteredApps = append(filteredApps, status)
		}
	}

	appAms, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, am := range appAms.Items {
		if am.Spec.AppOwner == owner && (am.Status.State == v1alpha1.Pending ||
			am.Status.State == v1alpha1.Downloading || am.Status.State == v1alpha1.Installing) {
			if !stateSet.Has(v1alpha1.Pending.String()) || !stateSet.Has(v1alpha1.Downloading.String()) || !stateSet.Has(v1alpha1.Installing.String()) {
				continue
			}
			if len(isSysApp) > 0 && isSysApp == "true" {
				continue
			}
			now := metav1.Now()
			status := appinstaller.Status{
				Name:              am.Spec.AppName,
				AppID:             utils.GetAppID(am.Spec.AppName),
				Namespace:         am.Spec.AppNamespace,
				CreationTimestamp: now,
				Source:            am.Spec.Source,
				AppStatus: v1alpha1.ApplicationStatus{
					State:      am.Status.State.String(),
					StatusTime: &now,
					UpdateTime: &now,
				},
			}
			if !appSets.Has(am.Spec.AppName) {
				filteredApps = append(filteredApps, status)
			}
		}
	}

	// sort by create time desc
	sort.Slice(filteredApps, func(i, j int) bool {
		return filteredApps[j].CreationTimestamp.Before(&filteredApps[i].CreationTimestamp)
	})

	resp.WriteAsJson(map[string]interface{}{"result": filteredApps})
}

func (h *Handler) operate(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var am v1alpha1.ApplicationManager
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)
	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	operate := appinstaller.Operate{
		AppName:           am.Spec.AppName,
		AppNamespace:      am.Spec.AppNamespace,
		AppOwner:          am.Spec.AppOwner,
		OpType:            am.Status.OpType,
		ResourceType:      am.Spec.Type.String(),
		State:             toProcessing(am.Status.State),
		Message:           am.Status.Message,
		CreationTimestamp: am.CreationTimestamp,
		Source:            am.Spec.Source,
		Progress:          am.Status.Progress,
	}

	resp.WriteAsJson(operate)
}

func (h *Handler) appsOperate(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	// run with request context for incoming client
	ams, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	filteredOperates := make([]appinstaller.Operate, 0)
	for _, am := range ams.Items {
		if am.Spec.Type != v1alpha1.App {
			continue
		}

		if am.Spec.AppOwner == owner {
			operate := appinstaller.Operate{
				AppName:           am.Spec.AppName,
				AppNamespace:      am.Spec.AppNamespace,
				AppOwner:          am.Spec.AppOwner,
				State:             toProcessing(am.Status.State),
				OpType:            am.Status.OpType,
				ResourceType:      am.Spec.Type.String(),
				Message:           am.Status.Message,
				CreationTimestamp: am.CreationTimestamp,
				Source:            am.Spec.Source,
				Progress:          am.Status.Progress,
			}
			filteredOperates = append(filteredOperates, operate)
		}
	}

	// sort by create time desc
	sort.Slice(filteredOperates, func(i, j int) bool {
		return filteredOperates[j].CreationTimestamp.Before(&filteredOperates[i].CreationTimestamp)
	})

	resp.WriteAsJson(map[string]interface{}{"result": filteredOperates})
}

func (h *Handler) operateHistory(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var am v1alpha1.ApplicationManager
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	key := types.NamespacedName{Name: name}
	err = h.ctrlClient.Get(req.Request.Context(), key, &am)

	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	ops := make([]appinstaller.OperateHistory, 0, len(am.Status.OpRecords))
	for _, r := range am.Status.OpRecords {
		op := appinstaller.OperateHistory{
			AppName:      am.Spec.AppName,
			AppNamespace: am.Spec.AppNamespace,
			AppOwner:     am.Spec.AppOwner,
			ResourceType: am.Spec.Type.String(),
			OpRecord: v1alpha1.OpRecord{
				OpType:     r.OpType,
				Message:    r.Message,
				Source:     r.Source,
				Version:    r.Version,
				Status:     r.Status,
				StatusTime: r.StatusTime,
			},
		}
		ops = append(ops, op)
	}

	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func (h *Handler) allOperateHistory(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute).(string)
	source := req.QueryParameter("source")
	resourceType := req.QueryParameter("resourceType")

	filteredSources := constants.Sources
	filteredResourceTypes := constants.ResourceTypes
	if len(source) > 0 {
		filteredSources = sets.String{}
		for _, s := range strings.Split(source, "|") {
			filteredSources.Insert(s)
		}
	}
	if len(resourceType) > 0 {
		filteredResourceTypes = sets.String{}
		for _, s := range strings.Split(resourceType, "|") {
			filteredResourceTypes.Insert(s)
		}
	}

	var ams v1alpha1.ApplicationManagerList
	err := h.ctrlClient.List(req.Request.Context(), &ams)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ops := make([]appinstaller.OperateHistory, 0)

	for _, am := range ams.Items {
		if !filteredResourceTypes.Has(am.Spec.Type.String()) {
			continue
		}
		if am.Spec.AppOwner != owner || userspace.IsSysApp(am.Spec.AppName) {
			continue
		}
		for _, r := range am.Status.OpRecords {
			if !filteredSources.Has(r.Source) {
				continue
			}
			op := appinstaller.OperateHistory{
				AppName:      am.Spec.AppName,
				AppNamespace: am.Spec.AppNamespace,
				AppOwner:     am.Spec.AppOwner,
				ResourceType: am.Spec.Type.String(),
				OpRecord: v1alpha1.OpRecord{
					OpType:     r.OpType,
					Message:    r.Message,
					Source:     r.Source,
					Version:    r.Version,
					Status:     r.Status,
					StatusTime: r.StatusTime,
				},
			}
			ops = append(ops, op)
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[j].StatusTime.Before(ops[i].StatusTime)
	})

	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func (h *Handler) getApp(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	appName := req.PathParameter(ParamAppName)
	name, err := utils.FmtAppMgrName(appName, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var app *v1alpha1.Application

	am, err := client.AppClient.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	var appConfig appinstaller.ApplicationConfig
	err = json.Unmarshal([]byte(am.Spec.Config), &appConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	now := metav1.Now()
	app = &v1alpha1.Application{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: am.CreationTimestamp,
		},
		Spec: v1alpha1.ApplicationSpec{
			Name:      am.Spec.AppName,
			Appid:     utils.GetAppID(am.Spec.AppName),
			IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
			Namespace: am.Spec.AppNamespace,
			Owner:     owner,
			Entrances: appConfig.Entrances,
			Icon:      appConfig.Icon,
		},
		Status: v1alpha1.ApplicationStatus{
			State:      am.Status.State.String(),
			StatusTime: &now,
			UpdateTime: &now,
		},
	}

	curApp, err := client.AppClient.AppV1alpha1().Applications().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		api.HandleError(resp, req, err)
		return
	}

	if apierrors.IsNotFound(err) && am.Status.State != v1alpha1.Pending &&
		am.Status.State != v1alpha1.Installing && am.Status.State != v1alpha1.Downloading {
		api.HandleNotFound(resp, req, err)
		return
	}

	if err == nil {
		app = curApp
	}

	resp.WriteAsJson(app)
}

func (h *Handler) apps(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	isSysApp := req.QueryParameter("issysapp")
	state := req.QueryParameter("state")

	ss := make([]string, 0)
	if state != "" {
		ss = strings.Split(state, "|")
	}
	stateSet := constants.States
	if len(ss) > 0 {
		stateSet = sets.String{}
	}
	for _, s := range ss {
		stateSet.Insert(s)
	}
	filteredApps := make([]v1alpha1.Application, 0)
	appsMap := make(map[string]v1alpha1.Application)

	// get pending app's from app managers
	ams, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	for _, am := range ams.Items {
		if am.Spec.Type != v1alpha1.App {
			continue
		}

		if am.Spec.AppOwner == owner && (am.Status.State == v1alpha1.Pending || am.Status.State == v1alpha1.Installing ||
			am.Status.State == v1alpha1.Downloading) {
			if !stateSet.Has(v1alpha1.Pending.String()) || !stateSet.Has(v1alpha1.Installing.String()) ||
				!stateSet.Has(v1alpha1.Downloading.String()) {
				continue
			}
			if len(isSysApp) > 0 && isSysApp == "true" {
				continue
			}
			var appconfig appinstaller.ApplicationConfig
			err = json.Unmarshal([]byte(am.Spec.Config), &appconfig)
			if err != nil {
				api.HandleError(resp, req, err)
				return
			}
			now := metav1.Now()
			name, _ := utils.FmtAppMgrName(am.Spec.AppName, owner, appconfig.Namespace)
			app := v1alpha1.Application{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					CreationTimestamp: am.CreationTimestamp,
				},
				Spec: v1alpha1.ApplicationSpec{
					Name:      am.Spec.AppName,
					Appid:     utils.GetAppID(am.Spec.AppName),
					IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
					Namespace: am.Spec.AppNamespace,
					Owner:     owner,
					Entrances: appconfig.Entrances,
					Icon:      appconfig.Icon,
				},
				Status: v1alpha1.ApplicationStatus{
					State:      am.Status.State.String(),
					StatusTime: &now,
					UpdateTime: &now,
				},
			}
			appsMap[app.Name] = app
		}
	}

	// run with request context for incoming client
	allApps, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	for _, a := range allApps.Items {
		if a.Spec.Owner == owner {
			if !stateSet.Has(a.Status.State) {
				continue
			}
			if len(isSysApp) > 0 && strconv.FormatBool(a.Spec.IsSysApp) != isSysApp {
				continue
			}
			appsMap[a.Name] = a
		}
	}

	for _, app := range appsMap {
		filteredApps = append(filteredApps, app)
	}

	// sort by create time desc
	sort.Slice(filteredApps, func(i, j int) bool {
		return filteredApps[j].CreationTimestamp.Before(&filteredApps[i].CreationTimestamp)
	})

	resp.WriteAsJson(filteredApps)
}

func (h *Handler) pendingOrInstallingApps(req *restful.Request, resp *restful.Response) {
	ams, err := utils.GetPendingOrRunningTask(req.Request.Context())
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(ams)
}

func (h *Handler) terminusVersion(req *restful.Request, resp *restful.Response) {
	terminus, err := upgrade.GetTerminusVersion(req.Request.Context(), h.ctrlClient)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(map[string]interface{}{"version": terminus.Spec.Version})
}

func (h *Handler) nodes(req *restful.Request, resp *restful.Response) {
	var nodes corev1.NodeList
	err := h.ctrlClient.List(req.Request.Context(), &nodes, &client.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(map[string]interface{}{"result": nodes.Items})
}

func toProcessing(state v1alpha1.ApplicationManagerState) v1alpha1.ApplicationManagerState {
	if state == v1alpha1.Installing || state == v1alpha1.Uninstalling ||
		state == v1alpha1.Upgrading || state == v1alpha1.Resuming ||
		state == v1alpha1.Canceling || state == v1alpha1.Stopping {
		return v1alpha1.Processing
	}
	return state
}

func (h *Handler) operateRecommend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var am v1alpha1.ApplicationManager
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)

	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	operate := appinstaller.Operate{
		AppName:           am.Spec.AppName,
		AppOwner:          am.Spec.AppOwner,
		OpType:            am.Status.OpType,
		ResourceType:      am.Spec.Type.String(),
		State:             toProcessing(am.Status.State),
		Message:           am.Status.Message,
		CreationTimestamp: am.CreationTimestamp,
		Source:            am.Spec.Source,
	}
	resp.WriteAsJson(operate)
}

func (h *Handler) operateRecommendList(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	ams, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	filteredOperates := make([]appinstaller.Operate, 0)
	for _, am := range ams.Items {
		if am.Spec.AppOwner == owner && am.Spec.Type == v1alpha1.Recommend {
			operate := appinstaller.Operate{
				AppName:           am.Spec.AppName,
				AppOwner:          am.Spec.AppOwner,
				State:             toProcessing(am.Status.State),
				OpType:            am.Status.OpType,
				ResourceType:      am.Spec.Type.String(),
				Message:           am.Status.Message,
				CreationTimestamp: am.CreationTimestamp,
				Source:            am.Spec.Source,
			}
			filteredOperates = append(filteredOperates, operate)
		}
	}
	// sort by create time desc
	sort.Slice(filteredOperates, func(i, j int) bool {
		return filteredOperates[j].CreationTimestamp.Before(&filteredOperates[i].CreationTimestamp)
	})

	resp.WriteAsJson(map[string]interface{}{"result": filteredOperates})
}

func (h *Handler) operateRecommendHistory(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var am v1alpha1.ApplicationManager
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	key := types.NamespacedName{Name: name}
	err = h.ctrlClient.Get(req.Request.Context(), key, &am)
	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}

	ops := make([]appinstaller.OperateHistory, 0, len(am.Status.OpRecords))
	for _, r := range am.Status.OpRecords {
		op := appinstaller.OperateHistory{
			AppName:      am.Spec.AppName,
			AppNamespace: am.Spec.AppNamespace,
			AppOwner:     am.Spec.AppOwner,
			ResourceType: am.Spec.Type.String(),

			OpRecord: v1alpha1.OpRecord{
				OpType:     r.OpType,
				Message:    r.Message,
				Source:     r.Source,
				Version:    r.Version,
				Status:     r.Status,
				StatusTime: r.StatusTime,
			},
		}
		ops = append(ops, op)
	}
	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func (h *Handler) allOperateRecommendHistory(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var ams v1alpha1.ApplicationManagerList
	err := h.ctrlClient.List(req.Request.Context(), &ams)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ops := make([]appinstaller.OperateHistory, 0)

	for _, am := range ams.Items {
		if am.Spec.AppOwner != owner || userspace.IsSysApp(am.Spec.AppName) || am.Spec.Type != v1alpha1.Recommend {
			continue
		}
		for _, r := range am.Status.OpRecords {
			op := appinstaller.OperateHistory{
				AppName:      am.Spec.AppName,
				AppNamespace: am.Spec.AppNamespace,
				AppOwner:     am.Spec.AppOwner,
				ResourceType: am.Spec.Type.String(),

				OpRecord: v1alpha1.OpRecord{
					OpType:     r.OpType,
					Message:    r.Message,
					Source:     r.Source,
					Version:    r.Version,
					Status:     r.Status,
					StatusTime: r.StatusTime,
				},
			}
			ops = append(ops, op)
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[j].StatusTime.Before(ops[i].StatusTime)
	})

	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func (h *Handler) allUsersApps(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	isSysApp := req.QueryParameter("issysapp")
	state := req.QueryParameter("state")

	//kClient, _ := utils.GetClient()
	role, err := kubesphere.GetUserRole(req.Request.Context(), h.kubeConfig, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	ss := make([]string, 0)
	if state != "" {
		ss = strings.Split(state, "|")
	}
	stateSet := constants.States
	if len(ss) > 0 {
		stateSet = sets.String{}
	}
	for _, s := range ss {
		stateSet.Insert(s)
	}

	filteredApps := make([]v1alpha1.Application, 0)
	appsMap := make(map[string]v1alpha1.Application)
	// get pending app's from app managers
	ams, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	for _, am := range ams.Items {
		if am.Spec.Type != v1alpha1.App {
			continue
		}
		if role != "platform-admin" && am.Spec.AppOwner != owner {
			continue
		}

		if am.Status.State == v1alpha1.Pending || am.Status.State == v1alpha1.Installing ||
			am.Status.State == v1alpha1.Downloading {
			if !stateSet.Has(v1alpha1.Pending.String()) || !stateSet.Has(v1alpha1.Installing.String()) ||
				!stateSet.Has(v1alpha1.Downloading.String()) {
				continue
			}
			if len(isSysApp) > 0 && isSysApp == "true" {
				continue
			}
			var appconfig appinstaller.ApplicationConfig
			err = json.Unmarshal([]byte(am.Spec.Config), &appconfig)
			if err != nil {
				api.HandleError(resp, req, err)
				return
			}
			now := metav1.Now()
			app := v1alpha1.Application{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:              am.Name,
					CreationTimestamp: am.CreationTimestamp,
				},
				Spec: v1alpha1.ApplicationSpec{
					Name:      am.Spec.AppName,
					Appid:     utils.GetAppID(am.Spec.AppName),
					IsSysApp:  userspace.IsSysApp(am.Spec.AppName),
					Namespace: am.Spec.AppNamespace,
					Owner:     am.Spec.AppOwner,
					Entrances: appconfig.Entrances,
					Icon:      appconfig.Icon,
				},
				Status: v1alpha1.ApplicationStatus{
					State:      am.Status.State.String(),
					StatusTime: &now,
					UpdateTime: &now,
				},
			}
			appsMap[fmt.Sprintf("%s-%s", am.Spec.AppName, am.Spec.AppOwner)] = app
		}
	}

	// run with request context for incoming client
	allApps, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	for _, a := range allApps.Items {
		if !stateSet.Has(a.Status.State) {
			continue
		}
		if role != "platform-admin" && a.Spec.Owner != owner {
			continue
		}
		if len(isSysApp) > 0 && strconv.FormatBool(a.Spec.IsSysApp) != isSysApp {
			continue
		}
		appsMap[fmt.Sprintf("%s-%s", a.Spec.Name, a.Spec.Owner)] = a
	}

	for _, app := range appsMap {
		filteredApps = append(filteredApps, app)
	}

	// sort by create time desc
	sort.Slice(filteredApps, func(i, j int) bool {
		return filteredApps[j].CreationTimestamp.Before(&filteredApps[i].CreationTimestamp)
	})

	resp.WriteAsJson(filteredApps)
}

func (h *Handler) getAllUser() ([]string, error) {
	users := make([]string, 0)
	gvr := schema.GroupVersionResource{
		Group:    "iam.kubesphere.io",
		Version:  "v1alpha2",
		Resource: "users",
	}
	dClient, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		return users, err
	}
	user, err := dClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return users, err
	}
	for _, u := range user.Items {
		if u.Object == nil {
			continue
		}
		users = append(users, u.GetName())
	}
	return users, nil
}
