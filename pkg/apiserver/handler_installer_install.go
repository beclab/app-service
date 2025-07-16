package apiserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/utils/config"
	"github.com/emicklei/go-restful/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type depRequest struct {
	Data []appcfg.Dependency `json:"data"`
}

func (h *Handler) install(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)

	marketSource := req.HeaderParameter(constants.MarketSource)
	klog.Infof("install: user: %v, source: %v", owner, marketSource)

	insReq := &api.InstallRequest{}
	err := req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}
	if insReq.Source != api.Market && insReq.Source != api.Custom && insReq.Source != api.DevBox {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported chart source: %s", insReq.Source))
		return
	}

	// (V2)TODO: get current user role and check if the app is installed by admin
	admin, err := kubesphere.GetAdminUsername(req.Request.Context(), h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	isAdmin, err := kubesphere.IsAdmin(req.Request.Context(), h.kubeConfig, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	appConfig, _, err := apputils.GetAppConfig(req.Request.Context(), app, owner,
		insReq.CfgURL, insReq.RepoURL, "", token, admin, marketSource, isAdmin)
	if err != nil {
		klog.Errorf("Failed to get appconfig err=%v", err)
		api.HandleBadRequest(resp, req, err)
		return
	}
	unSatisfiedDeps, _ := CheckDependencies(req.Request.Context(), appConfig.Dependencies, h.ctrlClient, owner, true)
	if len(unSatisfiedDeps) > 0 {
		api.HandleBadRequest(resp, req, FormatDependencyError(unSatisfiedDeps))
		return
	}

	installedConflictApp, err := CheckConflicts(req.Request.Context(), appConfig.Conflicts, owner)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	if len(installedConflictApp) > 0 {
		api.HandleBadRequest(resp, req, fmt.Errorf("this app conflict with those installed app: %v", installedConflictApp))
		return
	}

	err = apputils.CheckTailScaleACLs(appConfig.TailScale.ACLs)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	if !utils.MatchVersion(appConfig.CfgFileVersion, config.MinCfgFileVersion) {
		api.HandleBadRequest(resp, req, fmt.Errorf("olaresManifest.version must %s", config.MinCfgFileVersion))
		return
	}

	if apputils.IsForbidNamespace(appConfig.Namespace) {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported namespace: %s", appConfig.Namespace))
		return
	}

	client, _ := utils.GetClient()
	role, err := kubesphere.GetUserRole(req.Request.Context(), h.kubeConfig, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if role != "owner" && role != "admin" && appConfig.OnlyAdmin {
		api.HandleBadRequest(resp, req, errors.New("only admin user can install this app"))
		return
	}

	if appConfig.AppScope.ClusterScoped {
		if role != "owner" && role != "admin" {
			api.HandleBadRequest(resp, req, errors.New("only admin user can create cluster level app"))
			return
		}
		apps, err := client.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		for _, a := range apps.Items {
			if a.Spec.Name == appConfig.AppName && a.Spec.Settings["clusterScoped"] == "true" {
				api.HandleBadRequest(resp, req, errors.New("only one cluster scoped app can install in on cluster"))
				return
			}
		}
	}
	resourceType, err := CheckAppRequirement(h.kubeConfig, token, appConfig)
	if err != nil {
		klog.Errorf("Failed to check app requirement err=%v", err)
		resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: resourceType,
			Message:  err.Error(),
		})
		return
	}

	resourceType, err = CheckUserResRequirement(req.Request.Context(), h.kubeConfig, appConfig, owner)
	if err != nil {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: resourceType,
			Message:  err.Error(),
		})
		return
	}

	satisfied, err := CheckMiddlewareRequirement(req.Request.Context(), h.kubeConfig, appConfig.Middleware)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if !satisfied {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: "middleware",
			Message:  fmt.Sprintf("middleware requirement can not be satisfied"),
		})
		return
	}

	// create ApplicationManager
	config, err := json.Marshal(appConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var a *v1alpha1.ApplicationManager
	name, _ := apputils.FmtAppMgrName(app, owner, appConfig.Namespace)
	appMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:      app,
			AppNamespace: appConfig.Namespace,
			AppOwner:     owner,
			Config:       string(config),
			Source:       insReq.Source.String(),
			Type:         v1alpha1.App,
		},
	}
	a, err = client.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		a, err = client.AppV1alpha1().ApplicationManagers().Create(req.Request.Context(), appMgr, metav1.CreateOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	} else {
		// update Spec.Config
		patchData := map[string]interface{}{
			"spec": map[string]interface{}{
				"config": string(config),
				"source": insReq.Source.String(),
			},
		}
		patchByte, err := json.Marshal(patchData)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		_, err = client.AppV1alpha1().ApplicationManagers().Patch(req.Request.Context(), a.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		if !appstate.IsOperationAllowed(a.Status.State, v1alpha1.InstallOp) {
			api.HandleBadRequest(resp, req, fmt.Errorf("%s operation is not allowed for %s state", v1alpha1.InstallOp, a.Status.State))
			return
		}
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:  v1alpha1.InstallOp,
		State:   v1alpha1.Pending,
		OpID:    a.ResourceVersion,
		Message: "waiting for install",
		Payload: map[string]string{
			"token":        token,
			"cfgURL":       insReq.CfgURL,
			"repoURL":      insReq.RepoURL,
			"version":      appConfig.Version,
			"marketSource": marketSource,
		},
		Progress:   "0.00",
		StatusTime: &now,
		UpdateTime: &now,
		OpTime:     &now,
	}
	a, err = apputils.UpdateAppMgrStatus(name, status)

	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	utils.PublishAsync(a.Spec.AppOwner, a.Spec.AppName, string(a.Status.OpType), a.Status.OpID, v1alpha1.Pending.String(), "", nil)

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: status.OpID},
	})
}
