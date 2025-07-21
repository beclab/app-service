package apiserver

import (
	"encoding/json"
	"fmt"
	"slices"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/utils/config"

	"github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type upgradeHelperIntf interface {
	getAdminUsers() (admin []string, isAdmin bool, err error)
	getAppConfig(prevCfg *appcfg.ApplicationConfig, adminUsers []string, marketSource string, isAdmin bool) (err error)
	validate() error
	encodingAppCofnig() (string, error)
}

var _ upgradeHelperIntf = (upgradeHelperIntf)(nil)
var _ upgradeHelperIntf = (upgradeHelperIntf)(nil)

type upgradeHandlerHelper struct {
	h         *Handler
	req       *restful.Request
	resp      *restful.Response
	owner     string
	app       string
	request   *api.UpgradeRequest
	token     string
	appConfig *appcfg.ApplicationConfig
}

type upgradeHandlerHelperV2 struct {
	*upgradeHandlerHelper
}

func (h *upgradeHandlerHelper) getAdminUsers() (admins []string, isAdmin bool, err error) {
	admins, err = kubesphere.GetAdminUserList(h.req.Request.Context(), h.h.kubeConfig)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}
	isAdmin, err = kubesphere.IsAdmin(h.req.Request.Context(), h.h.kubeConfig, h.owner)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}

	return
}

func (h *upgradeHandlerHelper) getAppConfig(prevCfg *appcfg.ApplicationConfig, adminUsers []string, marketSource string, _ bool) (err error) {
	var admin string
	if !prevCfg.AppScope.ClusterScoped {
		// installed as non-admin
		admin = adminUsers[0]
		if len(adminUsers) > 1 {
			for _, user := range adminUsers {
				if user != h.owner {
					admin = user
					break
				}
			}
		}
	} else {
		admin = h.owner
	}
	appConfig, _, err := config.GetAppConfig(
		h.req.Request.Context(), h.app, h.owner, h.request.CfgURL, h.request.RepoURL,
		h.request.Version, h.token, admin, marketSource, prevCfg.AppScope.ClusterScoped,
	)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}

	h.appConfig = appConfig
	return nil
}

func (h *upgradeHandlerHelper) validate() error {
	if h.appConfig == nil {
		return fmt.Errorf("application config is nil")
	}

	err := apputils.CheckTailScaleACLs(h.appConfig.TailScale.ACLs)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return err
	}

	if !utils.MatchVersion(h.appConfig.CfgFileVersion, config.MinCfgFileVersion) {
		api.HandleBadRequest(h.resp, h.req, fmt.Errorf("olaresManifest.version must %s", config.MinCfgFileVersion))
		return err
	}

	return nil
}

func (h *upgradeHandlerHelper) encodingAppCofnig() (string, error) {
	encoding, err := json.Marshal(h.appConfig)
	if err != nil {
		klog.Errorf("Failed to marshal app config err=%v", err)
		api.HandleError(h.resp, h.req, err)
		return "", err
	}
	return string(encoding), nil
}

func (h *upgradeHandlerHelperV2) getAppConfig(prevCfg *appcfg.ApplicationConfig, adminUsers []string, marketSource string, isAdmin bool) (err error) {
	klog.Info("Getting app config for V2")
	if len(adminUsers) == 0 {
		err := fmt.Errorf("no admin users found")
		klog.Error(err)
		api.HandleError(h.resp, h.req, err)
		return err
	}

	var admin string
	if isAdmin {
		admin = h.owner
	} else {
		admin = adminUsers[0]
	}

	appConfig, _, err := config.GetAppConfig(
		h.req.Request.Context(), h.app, h.owner, h.request.CfgURL, h.request.RepoURL,
		h.request.Version, h.token, admin, marketSource, isAdmin,
	)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}

	h.appConfig = appConfig

	return nil
}

func (h *Handler) appUpgrade(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	marketSource := req.HeaderParameter(constants.MarketSource)

	request := &api.UpgradeRequest{}
	err := req.ReadEntity(request)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	if !slices.Contains([]api.AppSource{
		api.Market,
		api.Custom,
		api.DevBox,
		api.System,
	}, request.Source) {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported chart source: %s", request.Source))
		return
	}

	var appMgr appv1alpha1.ApplicationManager
	appMgrName, err := apputils.FmtAppMgrName(app, owner, "")
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

	apiVersion, err := apputils.GetApiVersionFromAppConfig(req.Request.Context(), app, owner,
		request.CfgURL, request.RepoURL, marketSource)
	if err != nil {
		klog.Errorf("Failed to get api version err=%v", err)
		api.HandleBadRequest(resp, req, err)
		return
	}

	var helper upgradeHelperIntf
	switch apiVersion {
	case appcfg.V1:
		klog.Info("Using install handler helper for V1")
		h := &upgradeHandlerHelper{
			h:       h,
			req:     req,
			resp:    resp,
			request: request,
			app:     app,
			owner:   owner,
			token:   token,
		}

		helper = h
	case appcfg.V2:
		klog.Info("Using install handler helper for V2")
		h := &upgradeHandlerHelperV2{
			upgradeHandlerHelper: &upgradeHandlerHelper{
				h:     h,
				req:   req,
				resp:  resp,
				app:   app,
				owner: owner,
				token: token,
			},
		}

		helper = h
	default:
		klog.Errorf("Unsupported app config api version: %s", apiVersion)
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported app config api version: %s", apiVersion))
		return
	}

	adminUsers, isAdmin, err := helper.getAdminUsers()
	if err != nil {
		klog.Errorf("Failed to get admin users err=%v", err)
		return
	}

	var prevCfg appcfg.ApplicationConfig
	err = appMgr.GetAppConfig(prevCfg)
	if err != nil {
		klog.Errorf("Failed to get previous app config err=%v", err)
		api.HandleError(resp, req, err)
		return
	}

	err = helper.getAppConfig(&prevCfg, adminUsers, marketSource, isAdmin)
	if err != nil {
		klog.Errorf("Failed to get app config err=%v", err)
		return
	}

	err = helper.validate()
	if err != nil {
		klog.Errorf("Failed to validate app config err=%v", err)
		return
	}

	appCopy := appMgr.DeepCopy()
	config, err := helper.encodingAppCofnig()
	if err != nil {
		klog.Errorf("Failed to encoding app config err=%v", err)
		return
	}

	appCopy.Spec.Config = config

	err = h.ctrlClient.Patch(req.Request.Context(), appCopy, client.MergeFrom(&appMgr))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	now := metav1.Now()
	status := appv1alpha1.ApplicationManagerStatus{
		OpType:  appv1alpha1.UpgradeOp,
		OpID:    appMgr.ResourceVersion,
		State:   appv1alpha1.Upgrading,
		Message: "waiting for upgrade",
		Payload: map[string]string{
			"cfgURL":       request.CfgURL,
			"repoURL":      request.RepoURL,
			"version":      request.Version,
			"token":        token,
			"marketSource": marketSource,
		},
		StatusTime: &now,
		UpdateTime: &now,
	}

	am, err := apputils.UpdateAppMgrStatus(appMgrName, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	utils.PublishAsync(am.Spec.AppOwner, am.Spec.AppName, string(am.Status.OpType), am.Status.OpID, appv1alpha1.Upgrading.String(), "", nil)

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: status.OpID},
	})
}
