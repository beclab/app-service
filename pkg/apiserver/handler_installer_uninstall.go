package apiserver

import (
	"errors"
	"fmt"

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

func (h *Handler) uninstall(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)

	if userspace.IsSysApp(app) {
		api.HandleBadRequest(resp, req, errors.New("sys app can not be uninstall"))
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

	//var application v1alpha1.Application
	//err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &application)
	//if err != nil {
	//	api.HandleError(resp, req, err)
	//	return
	//}
	//if application.Spec.IsSysApp {
	//	api.HandleBadRequest(resp, req, errors.New("can not uninstall sys app"))
	//	return
	//}
	if !appstate.IsOperationAllowed(am.Status.State, v1alpha1.UninstallOp) {
		api.HandleBadRequest(resp, req, fmt.Errorf("%s operation is not allowed for %s state", v1alpha1.UninstallOp, am.Status.State))
		return
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType: v1alpha1.UninstallOp,
		State:  v1alpha1.Uninstalling,
		OpID:   am.ResourceVersion,
		Payload: map[string]string{
			"token": token,
		},
		Progress:   "0.00",
		StatusTime: &now,
		UpdateTime: &now,
	}

	a, err := apputils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	utils.PublishAsync(a.Spec.AppOwner, a.Spec.AppName, string(a.Status.OpType), a.Status.OpID, v1alpha1.Uninstalling.String(), "", nil)

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: status.OpID},
	})
}
