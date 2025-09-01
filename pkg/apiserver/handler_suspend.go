package apiserver

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
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
	name, err := apputils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var am v1alpha1.ApplicationManager
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if !appstate.IsOperationAllowed(am.Status.State, v1alpha1.StopOp) {
		api.HandleBadRequest(resp, req, fmt.Errorf("%s operation is not allowed for %s state", v1alpha1.StopOp, am.Status.State))
		return
	}
	am.Spec.OpType = v1alpha1.StopOp
	err = h.ctrlClient.Update(req.Request.Context(), &am)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	opID := strconv.FormatInt(time.Now().Unix(), 10)

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.StopOp,
		OpID:       opID,
		State:      v1alpha1.Stopping,
		StatusTime: &now,
		UpdateTime: &now,
	}
	a, err := apputils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	utils.PublishAppEvent(a.Spec.AppOwner, a.Spec.AppName, string(a.Status.OpType), opID, v1alpha1.Stopping.String(), "", nil)

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: opID},
	})
}

func (h *Handler) resume(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	name, err := apputils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var am v1alpha1.ApplicationManager

	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if !appstate.IsOperationAllowed(am.Status.State, v1alpha1.ResumeOp) {
		api.HandleBadRequest(resp, req, fmt.Errorf("%s operation is not allowed for %s state", v1alpha1.UpgradeOp, am.Status.State))
		return
	}

	am.Spec.OpType = v1alpha1.ResumeOp
	err = h.ctrlClient.Update(req.Request.Context(), &am)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	now := metav1.Now()
	opID := strconv.FormatInt(time.Now().Unix(), 10)
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.ResumeOp,
		OpID:       opID,
		State:      v1alpha1.Resuming,
		StatusTime: &now,
		UpdateTime: &now,
	}
	a, err := apputils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	utils.PublishAppEvent(a.Spec.AppOwner, a.Spec.AppName, string(a.Status.OpType), opID, v1alpha1.Resuming.String(), "", nil)

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: opID},
	})
}
