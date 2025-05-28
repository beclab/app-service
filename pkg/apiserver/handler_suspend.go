package apiserver

import (
	"errors"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (h *Handler) suspend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	if userspace.IsSysApp(app) {
		api.HandleBadRequest(resp, req, errors.New("sys app can not be suspend"))
		return
	}
	var application v1alpha1.Application
	name, err := apputils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &application)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.StopOp,
		State:      v1alpha1.Stopping,
		StatusTime: &now,
		UpdateTime: &now,
	}
	_, err = apputils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) resume(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	var application v1alpha1.Application

	name, err := apputils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &application)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	isSuspendByController := false
	for _, e := range application.Status.EntranceStatuses {
		if e.Name == app && e.State == "suspend" {
			isSuspendByController = true
			break
		}
	}

	if application.Status.State != v1alpha1.AppSuspend.String() && !isSuspendByController {
		api.HandleBadRequest(resp, req, api.ErrNotSupportOperation)
		return
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.ResumeOp,
		State:      v1alpha1.Resuming,
		StatusTime: &now,
		UpdateTime: &now,
	}
	_, err = apputils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}
