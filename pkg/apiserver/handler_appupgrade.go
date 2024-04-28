package apiserver

import (
	"fmt"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (h *Handler) releaseVersion(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute)
	appNamespace := utils.AppNamespace(appName, owner.(string))
	actionConfig, _, err := helm.InitConfig(h.kubeConfig, appNamespace)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	version, _, err := utils.GetDeployedReleaseVersion(actionConfig, appName)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(api.ReleaseVersionResponse{
		Response: api.Response{Code: 200},
		Data:     api.ReleaseVersionData{Version: version},
	})
}

func (h *Handler) appUpgrade(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	request := &api.UpgradeRequest{}
	err := req.ReadEntity(request)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	if request.Source != api.Market && request.Source != api.Custom && request.Source != api.DevBox && request.Source != api.System {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported chart source: %s", request.Source))
		return
	}
	var appMgr appv1alpha1.ApplicationManager
	appMgrName := utils.FmtAppMgrName(app, owner)
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: appMgrName}, &appMgr)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if appMgr.Spec.Source != request.Source.String() {
		api.HandleBadRequest(resp, req, fmt.Errorf("unmatched chart source"))
		return
	}

	token := req.HeaderParameter(constants.AuthorizationTokenKey)

	now := metav1.Now()
	status := appv1alpha1.ApplicationManagerStatus{
		OpType:  appv1alpha1.UpgradeOp,
		State:   appv1alpha1.Pending,
		Message: "waiting for upgrade",
		Payload: map[string]string{
			"cfgURL":  request.CfgURL,
			"repoURL": request.RepoURL,
			"version": request.Version,
			"token":   token,
		},
		StatusTime: &now,
		UpdateTime: &now,
	}

	_, err = utils.UpdateAppMgrStatus(appMgrName, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}
