package apiserver

import (
	"encoding/json"
	"fmt"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *Handler) releaseVersion(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute)
	appNamespace, err := utils.AppNamespace(appName, owner.(string), "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

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
	appMgrName, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
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

	appConfig, _, err := GetAppConfig(req.Request.Context(), app, owner, request.CfgURL, request.RepoURL, request.Version, token)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if !utils.MatchVersion(appConfig.CfgFileVersion, MinCfgFileVersion) {
		api.HandleBadRequest(resp, req, fmt.Errorf("olaresManifest.version must %s", MinCfgFileVersion))
		return
	}

	config, err := json.Marshal(appConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var a appv1alpha1.ApplicationManager
	key := types.NamespacedName{Name: appMgrName}
	err = h.ctrlClient.Get(req.Request.Context(), key, &a)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	appCopy := a.DeepCopy()
	appCopy.Spec.Config = string(config)

	err = h.ctrlClient.Patch(req.Request.Context(), appCopy, client.MergeFrom(&a))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
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
