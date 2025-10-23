package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/utils/config"

	"github.com/emicklei/go-restful/v3"
	"helm.sh/helm/v3/pkg/time"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type depRequest struct {
	Data []appcfg.Dependency `json:"data"`
}

type installHelperIntf interface {
	getAdminUsers() (admin []string, isAdmin bool, err error)
	getInstalledApps() (installed bool, app []*v1alpha1.Application, err error)
	getAppConfig(adminUsers []string, marketSource string, isAdmin, appInstalled bool, installedApps []*v1alpha1.Application) (err error)
	validate(bool, []*v1alpha1.Application) error
	setAppEnv(overrides []sysv1alpha1.AppEnvVar) error
	applyAppEnv(ctx context.Context) error
	applyApplicationManager(marketSource string) (opID string, err error)
}

var _ installHelperIntf = (*installHandlerHelper)(nil)
var _ installHelperIntf = (*installHandlerHelperV2)(nil)

type installHandlerHelper struct {
	h                    *Handler
	req                  *restful.Request
	resp                 *restful.Response
	app                  string
	owner                string
	token                string
	insReq               *api.InstallRequest
	appConfig            *appcfg.ApplicationConfig
	client               *versioned.Clientset
	validateClusterScope func(isAdmin bool, installedApps []*v1alpha1.Application) (err error)
}

type installHandlerHelperV2 struct {
	installHandlerHelper
}

func (h *Handler) install(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	var err error
	token, err := h.GetUserServiceAccountToken(req.Request.Context(), owner)
	if err != nil {
		klog.Error("Failed to get user service account token: ", err)
		api.HandleError(resp, req, err)
		return
	}

	marketSource := req.HeaderParameter(constants.MarketSource)
	klog.Infof("install: user: %v, source: %v", owner, marketSource)

	insReq := &api.InstallRequest{}
	err = req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}
	klog.Infof("insReq: images: %v", insReq.Images)
	if insReq.Source != api.Market && insReq.Source != api.Custom && insReq.Source != api.DevBox {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported chart source: %s", insReq.Source))
		return
	}

	apiVersion, err := apputils.GetApiVersionFromAppConfig(req.Request.Context(), &apputils.ConfigOptions{
		App:          app,
		Owner:        owner,
		RepoURL:      insReq.RepoURL,
		MarketSource: marketSource,
	})
	if err != nil {
		klog.Errorf("Failed to get api version err=%v", err)
		api.HandleBadRequest(resp, req, err)
		return
	}

	client, err := utils.GetClient()
	if err != nil {
		klog.Errorf("Failed to get client err=%v", err)
		api.HandleError(resp, req, err)
		return
	}

	var helper installHelperIntf
	switch apiVersion {
	case appcfg.V1:
		klog.Info("Using install handler helper for V1")
		h := &installHandlerHelper{
			h:      h,
			req:    req,
			resp:   resp,
			app:    app,
			owner:  owner,
			token:  token,
			insReq: insReq,
			client: client,
		}

		h.validateClusterScope = h._validateClusterScope

		helper = h
	case appcfg.V2:
		klog.Info("Using install handler helper for V2")
		h := &installHandlerHelperV2{
			installHandlerHelper: installHandlerHelper{
				h:      h,
				req:    req,
				resp:   resp,
				app:    app,
				owner:  owner,
				token:  token,
				insReq: insReq,
				client: client,
			},
		}

		h.validateClusterScope = h._validateClusterScope
		helper = h
	default:
		klog.Errorf("Unsupported app config api version: %s", apiVersion)
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported app config api version: %s", apiVersion))
		return
	}

	adminUsers, isAdmin, err := helper.getAdminUsers()
	if err != nil {
		klog.Errorf("Failed to get admin user err=%v", err)
		return
	}

	// V2: get current user role and check if the app is installed by admin
	appInstalled, installedApps, err := helper.getInstalledApps()
	if err != nil {
		klog.Errorf("Failed to get installed app err=%v", err)
		return
	}

	err = helper.getAppConfig(adminUsers, marketSource, isAdmin, appInstalled, installedApps)
	if err != nil {
		klog.Errorf("Failed to get app config err=%v", err)
		return
	}

	err = helper.setAppEnv(insReq.Envs)
	if err != nil {
		klog.Errorf("Failed to set app env err=%v", err)
		return
	}

	err = helper.validate(isAdmin, installedApps)
	if err != nil {
		klog.Errorf("Failed to validate app install request err=%v", err)
		return
	}

	err = helper.applyAppEnv(req.Request.Context())
	if err != nil {
		klog.Errorf("Failed to apply app env err=%v", err)
		return
	}

	// create ApplicationManager
	opID, err := helper.applyApplicationManager(marketSource)
	if err != nil {
		klog.Errorf("Failed to apply application manager err=%v", err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app, OpID: opID},
	})
}

func (h *installHandlerHelper) getAdminUsers() (admin []string, isAdmin bool, err error) {
	adminList, err := kubesphere.GetAdminUserList(h.req.Request.Context(), h.h.kubeConfig)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}

	for _, user := range adminList {
		admin = append(admin, user.Name)
		if user.Name == h.owner {
			isAdmin = true
		}
	}

	return
}

func (h *installHandlerHelper) validate(isAdmin bool, installedApps []*v1alpha1.Application) (err error) {
	unSatisfiedDeps, err := CheckDependencies(h.req.Request.Context(), h.appConfig.Dependencies, h.h.ctrlClient, h.owner, true)

	responseBadRequest := func(e error) {
		err = e
		api.HandleBadRequest(h.resp, h.req, err)
	}
	err = apputils.CheckDependencies2(h.req.Request.Context(), h.h.ctrlClient, h.appConfig.Dependencies, h.owner, true)
	if err != nil {
		klog.Errorf("Failed to check dependencies err=%v", err)
		responseBadRequest(FormatDependencyError(unSatisfiedDeps))
		return
	}

	err = apputils.CheckConflicts(h.req.Request.Context(), h.appConfig.Conflicts, h.owner)
	if err != nil {
		klog.Errorf("Failed to check installed conflict app err=%v", err)
		api.HandleBadRequest(h.resp, h.req, err)
		return
	}

	err = apputils.CheckTailScaleACLs(h.appConfig.TailScale.ACLs)
	if err != nil {
		klog.Errorf("Failed to check TailScale ACLs err=%v", err)
		api.HandleBadRequest(h.resp, h.req, err)
		return
	}

	err = apputils.CheckCfgFileVersion(h.appConfig.CfgFileVersion, config.MinCfgFileVersion)
	if err != nil {
		responseBadRequest(err)
		return
	}

	err = apputils.CheckNamespace(h.appConfig.Namespace)
	if err != nil {
		responseBadRequest(err)
		return
	}

	if !isAdmin && h.appConfig.OnlyAdmin {
		responseBadRequest(errors.New("only admin user can install this app"))
		return
	}

	if !isAdmin && h.appConfig.AppScope.ClusterScoped {
		responseBadRequest(errors.New("only admin user can create cluster level app"))
		return
	}

	if err = h.validateClusterScope(isAdmin, installedApps); err != nil {
		klog.Errorf("Failed to validate cluster scope err=%v", err)
		api.HandleBadRequest(h.resp, h.req, err)
		return
	}

	//resourceType, err := CheckAppRequirement(h.h.kubeConfig, h.token, h.appConfig)
	resourceType, err := apputils.CheckAppRequirement(h.token, h.appConfig)
	if err != nil {
		klog.Errorf("Failed to check app requirement err=%v", err)
		h.resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: resourceType,
			Message:  err.Error(),
		})
		return
	}

	resourceType, err = apputils.CheckUserResRequirement(h.req.Request.Context(), h.appConfig, h.owner)
	if err != nil {
		h.resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: resourceType,
			Message:  err.Error(),
		})
		return
	}

	satisfied, err := apputils.CheckMiddlewareRequirement(h.req.Request.Context(), h.h.ctrlClient, h.appConfig.Middleware)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}
	if !satisfied {
		err = fmt.Errorf("middleware requirement can not be satisfied")
		h.resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: "middleware",
			Message:  "middleware requirement can not be satisfied",
		})
		return
	}
	ret, err := apputils.CheckAppEnvs(h.req.Request.Context(), h.h.ctrlClient, h.appConfig.Envs, h.owner)
	if err != nil {
		klog.Errorf("Failed to check app environment config err=%v", err)
		api.HandleInternalError(h.resp, h.req, err)
		return
	}
	if ret != nil {
		api.HandleFailedCheck(h.resp, api.CheckTypeAppEnv, ret)
		return fmt.Errorf("Invalid appenv config, check result: %#v", ret)
	}

	return
}

func (h *installHandlerHelper) _validateClusterScope(isAdmin bool, installedApp []*v1alpha1.Application) (err error) {
	for _, installedApp := range installedApp {
		if h.appConfig.AppScope.ClusterScoped && installedApp.IsClusterScoped() {
			return errors.New("only one cluster scoped app can install in on cluster")
		}
	}

	return
}

func (h *installHandlerHelper) getInstalledApps() (installed bool, app []*v1alpha1.Application, err error) {
	var apps *v1alpha1.ApplicationList
	apps, err = h.client.AppV1alpha1().Applications().List(h.req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to list applications err=%v", err)
		api.HandleError(h.resp, h.req, err)
		return
	}

	for _, a := range apps.Items {
		if a.Spec.Name == h.app {
			installed = true
			app = append(app, &a)
		}
	}

	return
}

func (h *installHandlerHelper) getAppConfig(adminUsers []string, marketSource string, isAdmin, appInstalled bool, installedApps []*v1alpha1.Application) (err error) {
	var (
		admin                   string
		installAsAdmin          bool
		cluserAppInstalled      bool
		installedCluserAppOwner string
	)

	if appInstalled && len(installedApps) > 0 {
		for _, installedApp := range installedApps {
			klog.Infof("app: %s is already installed by %s", installedApp.Spec.Name, installedApp.Spec.Owner)
			// if the app is already installed, and the app's owner is admin,
			appOwner := installedApp.Spec.Owner
			if slices.Contains(adminUsers, appOwner) {
				// check the app is installed as cluster scope
				if installedApp.IsClusterScoped() {
					cluserAppInstalled = true
					installedCluserAppOwner = appOwner
				}
			}
		}
	}

	switch {
	case cluserAppInstalled:
		admin = installedCluserAppOwner
		installAsAdmin = false
	case !isAdmin:
		if len(adminUsers) == 0 {
			klog.Errorf("No admin user found")
			api.HandleBadRequest(h.resp, h.req, fmt.Errorf("no admin user found"))
			return
		}
		admin = adminUsers[0]
		installAsAdmin = false
	default:
		admin = h.owner
		installAsAdmin = true
	}

	appConfig, _, err := apputils.GetAppConfig(h.req.Request.Context(), &apputils.ConfigOptions{
		App:          h.app,
		Owner:        h.owner,
		RepoURL:      h.insReq.RepoURL,
		Version:      "",
		Admin:        admin,
		IsAdmin:      installAsAdmin,
		MarketSource: marketSource,
	})
	if err != nil {
		klog.Errorf("Failed to get appconfig err=%v", err)
		api.HandleBadRequest(h.resp, h.req, err)
		return
	}

	h.appConfig = appConfig

	return
}

func (h *installHandlerHelper) applyApplicationManager(marketSource string) (opID string, err error) {
	config, err := json.Marshal(h.appConfig)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}
	var a *v1alpha1.ApplicationManager
	name, _ := apputils.FmtAppMgrName(h.app, h.owner, h.appConfig.Namespace)
	images := make([]api.Image, 0)
	if len(h.insReq.Images) != 0 {
		images = h.insReq.Images
	}
	imagesStr, _ := json.Marshal(images)
	appMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				api.AppTokenKey:                 h.token,
				api.AppRepoURLKey:               h.insReq.RepoURL,
				api.AppVersionKey:               h.appConfig.Version,
				api.AppMarketSourceKey:          marketSource,
				api.AppInstallSourceKey:         "app-service",
				constants.ApplicationTitleLabel: h.appConfig.Title,
				constants.ApplicationImageLabel: string(imagesStr),
			},
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:      h.app,
			AppNamespace: h.appConfig.Namespace,
			AppOwner:     h.owner,
			Config:       string(config),
			Source:       h.insReq.Source.String(),
			Type:         v1alpha1.Type(h.appConfig.Type),
			OpType:       v1alpha1.InstallOp,
		},
	}
	a, err = h.client.AppV1alpha1().ApplicationManagers().Get(h.req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(h.resp, h.req, err)
			return
		}
		_, err = h.client.AppV1alpha1().ApplicationManagers().Create(h.req.Request.Context(), appMgr, metav1.CreateOptions{})
		if err != nil {
			api.HandleError(h.resp, h.req, err)
			return
		}
	} else {
		if !appstate.IsOperationAllowed(a.Status.State, v1alpha1.InstallOp) {
			err = fmt.Errorf("%s operation is not allowed for %s state", v1alpha1.InstallOp, a.Status.State)
			api.HandleBadRequest(h.resp, h.req, err)
			return
		}
		// update Spec.Config
		patchData := map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					api.AppTokenKey:                 h.token,
					api.AppRepoURLKey:               h.insReq.RepoURL,
					api.AppVersionKey:               h.appConfig.Version,
					api.AppMarketSourceKey:          marketSource,
					api.AppInstallSourceKey:         "app-service",
					constants.ApplicationTitleLabel: h.appConfig.Title,
				},
			},
			"spec": map[string]interface{}{
				"opType": v1alpha1.InstallOp,
				"config": string(config),
				"source": h.insReq.Source.String(),
			},
		}
		var patchByte []byte
		patchByte, err = json.Marshal(patchData)
		if err != nil {
			api.HandleError(h.resp, h.req, err)
			return
		}
		_, err = h.client.AppV1alpha1().ApplicationManagers().Patch(h.req.Request.Context(), a.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
		if err != nil {
			api.HandleError(h.resp, h.req, err)
			return
		}

	}

	opID = strconv.FormatInt(time.Now().Unix(), 10)

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.InstallOp,
		State:      v1alpha1.Pending,
		OpID:       opID,
		Message:    "waiting for install",
		Progress:   "0.00",
		StatusTime: &now,
		UpdateTime: &now,
		OpTime:     &now,
	}
	a, err = apputils.UpdateAppMgrStatus(name, status)

	if err != nil {
		api.HandleError(h.resp, h.req, err)
		return
	}

	utils.PublishAppEvent(a.Spec.AppOwner, a.Spec.AppName, string(a.Status.OpType), opID, v1alpha1.Pending.String(), "", nil)
	return
}

func (h *installHandlerHelper) setAppEnv(overrides []sysv1alpha1.AppEnvVar) (err error) {
	defer func() {
		if err != nil {
			api.HandleBadRequest(h.resp, h.req, err)
		}
	}()
	if len(overrides) == 0 {
		return nil
	}
	if h.appConfig == nil {
		return fmt.Errorf("refuse to set app env on nil appconfig")
	}
	if len(h.appConfig.Envs) == 0 {
		return fmt.Errorf("refuse to set app env on app: %s with no declared envs", h.appConfig.AppName)
	}
	for _, override := range overrides {
		var found bool
		for i := range h.appConfig.Envs {
			if h.appConfig.Envs[i].EnvName == override.EnvName {
				found = true
				h.appConfig.Envs[i].Value = override.Value
			}
		}
		if !found {
			return fmt.Errorf("app env '%s' not found in app config", override.EnvName)
		}
	}
	return nil
}

func (h *installHandlerHelper) applyAppEnv(ctx context.Context) (err error) {
	_, err = apputils.ApplyAppEnv(ctx, h.h.ctrlClient, h.appConfig)
	if err != nil {
		api.HandleError(h.resp, h.req, err)
	}
	return
}

func (h *installHandlerHelperV2) _validateClusterScope(isAdmin bool, installedApps []*v1alpha1.Application) (err error) {
	klog.Info("validate cluster scope for install handler v2")

	// check if subcharts has a client chart
	for _, subChart := range h.appConfig.SubCharts {
		if !subChart.Shared {
			if subChart.Name != h.app {
				err := fmt.Errorf("non-shared subchart must has the same name with the app, subchart name is %s but the main app is %s", subChart.Name, h.app)
				klog.Error(err)
				api.HandleBadRequest(h.resp, h.req, err)
				return err
			}
		}
	}

	// in V2, we do not check cluster scope here, the cluster scope app
	// will be checked if the cluster part is installed by another user in the installing phase

	return nil
}

func (h *installHandlerHelperV2) getAppConfig(adminUsers []string, marketSource string, isAdmin, appInstalled bool, installedApps []*v1alpha1.Application) (err error) {
	klog.Info("get app config for install handler v2")

	var (
		admin string
	)

	if isAdmin {
		admin = h.owner
	} else {
		if len(adminUsers) == 0 {
			klog.Errorf("No admin user found")
			api.HandleBadRequest(h.resp, h.req, fmt.Errorf("no admin user found"))
			return
		}
		admin = adminUsers[0]
	}

	appConfig, _, err := apputils.GetAppConfig(h.req.Request.Context(), &apputils.ConfigOptions{
		App:          h.app,
		Owner:        h.owner,
		RepoURL:      h.insReq.RepoURL,
		Version:      "",
		Token:        h.token,
		Admin:        admin,
		MarketSource: marketSource,
		IsAdmin:      isAdmin,
	})
	if err != nil {
		klog.Errorf("Failed to get appconfig err=%v", err)
		api.HandleBadRequest(h.resp, h.req, err)
		return
	}

	h.appConfig = appConfig

	return
}

func (h *Handler) isDeployAllowed(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	name := fmt.Sprintf("%s-%s-%s", app, owner, app)
	var am v1alpha1.ApplicationManager
	err := h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &am)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		resp.WriteEntity(api.CanDeployResponse{
			Response: api.Response{Code: 200},
			Data: api.CanDeployResponseData{
				CanOp: true,
			},
		})
		return
	}
	if am.Status.State == v1alpha1.Uninstalled {
		resp.WriteEntity(api.CanDeployResponse{
			Response: api.Response{Code: 200},
			Data: api.CanDeployResponseData{
				CanOp: true,
			},
		})
		return
	}

	canOp := false
	if appstate.IsOperationAllowed(am.Status.State, v1alpha1.UninstallOp) {
		canOp = true
	}
	resp.WriteEntity(api.CanDeployResponse{
		Response: api.Response{Code: 200},
		Data: api.CanDeployResponseData{
			CanOp: canOp,
		},
	})
}
