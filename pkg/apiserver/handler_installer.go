package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"bytetrade.io/web3os/app-service/pkg/workflowinstaller"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type depRequest struct {
	Data []appinstaller.Dependency `json:"data"`
}

func (h *Handler) install(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)

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
	admin, err := kubesphere.GetAdminUsername(req.Request.Context(), h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	appConfig, _, err := GetAppConfig(req.Request.Context(), app, owner,
		insReq.CfgURL, insReq.RepoURL, "", token, admin)
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
		api.HandleError(resp, req, err)
		return
	}

	if len(installedConflictApp) > 0 {
		api.HandleBadRequest(resp, req, fmt.Errorf("this app conflict with those installed app: %v", installedConflictApp))
		return
	}

	err = utils.CheckTailScaleACLs(appConfig.TailScale.ACLs)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	if !utils.MatchVersion(appConfig.CfgFileVersion, MinCfgFileVersion) {
		api.HandleBadRequest(resp, req, fmt.Errorf("olaresManifest.version must %s", MinCfgFileVersion))
		return
	}

	if utils.IsForbidNamespace(appConfig.Namespace) {
		api.HandleBadRequest(resp, req, fmt.Errorf("unsupported namespace: %s", appConfig.Namespace))
		return
	}

	client, _ := utils.GetClient()
	role, err := kubesphere.GetUserRole(req.Request.Context(), h.kubeConfig, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if role != "platform-admin" && appConfig.OnlyAdmin {
		api.HandleBadRequest(resp, req, errors.New("only admin user can install this app"))
		return
	}

	if appConfig.AppScope.ClusterScoped {
		if role != "platform-admin" {
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
	name, _ := utils.FmtAppMgrName(app, owner, appConfig.Namespace)
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
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:  v1alpha1.InstallOp,
		State:   v1alpha1.Pending,
		Message: "waiting for install",
		Payload: map[string]string{
			"token":   token,
			"cfgURL":  insReq.CfgURL,
			"repoURL": insReq.RepoURL,
			"version": appConfig.Version,
		},
		Progress:   "0.00",
		StatusTime: &now,
		UpdateTime: &now,
	}
	a, err = utils.UpdateAppMgrStatus(name, status)

	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) uninstall(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)

	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var application v1alpha1.Application
	err = h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Name: name}, &application)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if application.Spec.IsSysApp {
		api.HandleBadRequest(resp, req, errors.New("can not uninstall sys app"))
		return
	}
	if !(application.Status.State == v1alpha1.AppSuspend.String() ||
		application.Status.State == v1alpha1.AppResuming.String() ||
		application.Status.State == v1alpha1.AppRunning.String()) {
		api.HandleBadRequest(resp, req, api.ErrNotSupportOperation)
		return
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType: v1alpha1.UninstallOp,
		Payload: map[string]string{
			"token": token,
		},
		Progress:   "0.00",
		StatusTime: &now,
		UpdateTime: &now,
	}

	_, err = utils.UpdateAppMgrStatus(name, status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) cancel(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	// type = timeout | operate
	cancelType := req.QueryParameter("type")
	if cancelType == "" {
		cancelType = "operate"
	}

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.CancelOp,
		Progress:   "0.00",
		Message:    cancelType,
		StatusTime: &now,
		UpdateTime: &now,
	}
	name, err := utils.FmtAppMgrName(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	_, err = utils.UpdateAppMgrStatus(name, status)

	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteAsJson(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

// UpdateAppState update applicationmanager state, message
func (h *Handler) UpdateAppState(ctx context.Context, name string, state v1alpha1.ApplicationManagerState, message string) error {
	var appMgr v1alpha1.ApplicationManager
	key := types.NamespacedName{Name: name}
	err := h.ctrlClient.Get(ctx, key, &appMgr)
	if err != nil {
		return err
	}
	appMgrCopy := appMgr.DeepCopy()
	now := metav1.Now()
	appMgr.Status.State = state
	appMgr.Status.Message = message
	appMgr.Status.StatusTime = &now
	appMgr.Status.UpdateTime = &now
	err = h.ctrlClient.Status().Patch(ctx, &appMgr, client.MergeFrom(appMgrCopy))
	return err
}

func (h *Handler) checkDependencies(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute) // get owner from request token
	var err error
	depReq := depRequest{}
	err = req.ReadEntity(&depReq)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	unSatisfiedDeps, _ := CheckDependencies(req.Request.Context(), depReq.Data, h.ctrlClient, owner.(string), true)
	klog.Infof("Check application dependencies unSatisfiedDeps=%v", unSatisfiedDeps)

	data := make([]api.DependenciesRespData, 0)
	for _, dep := range unSatisfiedDeps {
		data = append(data, api.DependenciesRespData{
			Name:    dep.Name,
			Version: dep.Version,
			Type:    dep.Type,
		})
	}
	resp.WriteEntity(api.DependenciesResp{
		Response: api.Response{Code: 200},
		Data:     data,
	})
}

// GetAppConfig get app installation configuration from app store
func GetAppConfig(ctx context.Context, app, owner, cfgURL, repoURL, version, token, admin string) (*appinstaller.ApplicationConfig, string, error) {
	if repoURL == "" {
		return nil, "", fmt.Errorf("url info is empty, cfg [%s], repo [%s]", cfgURL, repoURL)
	}

	var (
		appcfg    *appinstaller.ApplicationConfig
		chartPath string
		err       error
	)

	if cfgURL != "" {
		appcfg, chartPath, err = getAppConfigFromURL(ctx, app, cfgURL)
		if err != nil {
			return nil, "", err
		}
	} else {
		appcfg, chartPath, err = getAppConfigFromRepo(ctx, app, repoURL, version, token, owner, admin)
		if err != nil {
			return nil, chartPath, err
		}
	}

	// set appcfg.Namespace to specified namespace by OlaresManifests.Spec
	var namespace string
	if appcfg.Namespace != "" {
		namespace, _ = utils.AppNamespace(app, owner, appcfg.Namespace)
	} else {
		namespace = fmt.Sprintf("%s-%s", app, owner)
	}

	appcfg.Namespace = namespace
	appcfg.OwnerName = owner
	appcfg.RepoURL = repoURL
	return appcfg, chartPath, nil
}

func getAppConfigFromConfigurationFile(app, chart, owner, admin string) (*appinstaller.ApplicationConfig, string, error) {
	data, err := utils.RenderManifest(filepath.Join(chart, AppCfgFileName), owner, admin)
	if err != nil {
		return nil, chart, err
	}

	var cfg appinstaller.AppConfiguration
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, chart, err
	}

	return toApplicationConfig(app, chart, &cfg)
}

func getAppConfigFromURL(ctx context.Context, app, url string) (*appinstaller.ApplicationConfig, string, error) {
	client := resty.New().SetTimeout(2 * time.Second)
	resp, err := client.R().Get(url)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode() >= 400 {
		return nil, "", fmt.Errorf("app config url returns unexpected status code, %d", resp.StatusCode())
	}

	var cfg appinstaller.AppConfiguration
	if err := yaml.Unmarshal(resp.Body(), &cfg); err != nil {
		return nil, "", err
	}

	return toApplicationConfig(app, app, &cfg)
}

func getAppConfigFromRepo(ctx context.Context, app, repoURL, version, token, owner, admin string) (*appinstaller.ApplicationConfig, string, error) {
	chartPath, err := GetIndexAndDownloadChart(ctx, app, repoURL, version, token)
	if err != nil {
		return nil, chartPath, err
	}
	return getAppConfigFromConfigurationFile(app, chartPath, owner, admin)
}

func toApplicationConfig(app, chart string, cfg *appinstaller.AppConfiguration) (*appinstaller.ApplicationConfig, string, error) {
	var permission []appinstaller.AppPermission
	if cfg.Permission.AppData {
		permission = append(permission, appinstaller.AppDataRW)
	}
	if cfg.Permission.AppCache {
		permission = append(permission, appinstaller.AppCacheRW)
	}
	if len(cfg.Permission.UserData) > 0 {
		permission = append(permission, appinstaller.UserDataRW)
	}

	if len(cfg.Permission.SysData) > 0 {
		var perm []appinstaller.SysDataPermission
		for _, s := range cfg.Permission.SysData {
			perm = append(perm, appinstaller.SysDataPermission{
				AppName:   s.AppName,
				Svc:       s.Svc,
				Namespace: s.Namespace,
				Port:      s.Port,
				Group:     s.Group,
				DataType:  s.DataType,
				Version:   s.Version,
				Ops:       s.Ops,
			})
		}
		permission = append(permission, perm)
	}

	valuePtr := func(v resource.Quantity, err error) (*resource.Quantity, error) {
		if errors.Is(err, resource.ErrFormatWrong) {
			return nil, nil
		}

		return &v, nil
	}

	mem, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredMemory))
	if err != nil {
		return nil, chart, err
	}

	disk, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredDisk))
	if err != nil {
		return nil, chart, err
	}

	cpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredCPU))
	if err != nil {
		return nil, chart, err
	}

	gpu, err := valuePtr(resource.ParseQuantity(cfg.Spec.RequiredGPU))
	if err != nil {
		return nil, chart, err
	}

	// transform from Policy to AppPolicy
	var policies []appinstaller.AppPolicy
	for _, p := range cfg.Options.Policies {
		d, _ := time.ParseDuration(p.Duration)
		policies = append(policies, appinstaller.AppPolicy{
			EntranceName: p.EntranceName,
			URIRegex:     p.URIRegex,
			Level:        p.Level,
			OneTime:      p.OneTime,
			Duration:     d,
		})
	}

	// check dependencies version format
	for _, dep := range cfg.Options.Dependencies {
		if err = checkVersionFormat(dep.Version); err != nil {
			return nil, chart, err
		}
	}

	if cfg.Middleware != nil && cfg.Middleware.Redis != nil {
		if len(cfg.Middleware.Redis.Namespace) == 0 {
			return nil, chart, errors.New("middleware of Redis namespace can not be empty")
		}
	}
	var appid string
	if userspace.IsSysApp(app) {
		appid = app
	} else {
		appid = utils.Md5String(app)[:8]
	}

	return &appinstaller.ApplicationConfig{
		AppID:          appid,
		CfgFileVersion: cfg.ConfigVersion,
		AppName:        app,
		Title:          cfg.Metadata.Title,
		Version:        cfg.Metadata.Version,
		Target:         cfg.Metadata.Target,
		ChartsName:     chart,
		Entrances:      cfg.Entrances,
		Ports:          cfg.Ports,
		TailScale:      cfg.TailScale,
		Icon:           cfg.Metadata.Icon,
		Permission:     permission,
		Requirement: appinstaller.AppRequirement{
			Memory: mem,
			CPU:    cpu,
			Disk:   disk,
			GPU:    gpu,
		},
		Policies:             policies,
		Middleware:           cfg.Middleware,
		AnalyticsEnabled:     cfg.Options.Analytics.Enabled,
		ResetCookieEnabled:   cfg.Options.ResetCookie.Enabled,
		Dependencies:         cfg.Options.Dependencies,
		Conflicts:            cfg.Options.Conflicts,
		AppScope:             cfg.Options.AppScope,
		WsConfig:             cfg.Options.WsConfig,
		Upload:               cfg.Options.Upload,
		OnlyAdmin:            cfg.Spec.OnlyAdmin,
		Namespace:            cfg.Spec.Namespace,
		MobileSupported:      cfg.Options.MobileSupported,
		OIDC:                 cfg.Options.OIDC,
		ApiTimeout:           cfg.Options.ApiTimeout,
		RunAsUser:            cfg.Spec.RunAsUser,
		AllowedOutboundPorts: cfg.Options.AllowedOutboundPorts,
	}, chart, nil
}

func (h *Handler) installRecommend(req *restful.Request, resp *restful.Response) {
	insReq := &api.InstallRequest{}
	err := req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	app := req.PathParameter(ParamWorkflowName)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	klog.Infof("Download chart and get workflow config appName=%s repoURL=%s", app, insReq.RepoURL)
	workflowCfg, err := getWorkflowConfigFromRepo(req.Request.Context(), owner, app, insReq.RepoURL, "", token)
	if err != nil {
		klog.Error("Failed to get workflow config appName=%s repoURL=%s err=%v", app, insReq.RepoURL, err)
		api.HandleError(resp, req, err)
		return
	}

	satisfied, err := CheckMiddlewareRequirement(req.Request.Context(), h.kubeConfig, workflowCfg.Cfg.Middleware)
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

	go h.notifyKnowledgeInstall(workflowCfg.Cfg.Metadata.Title, app, owner)

	client, _ := utils.GetClient()

	var a *v1alpha1.ApplicationManager
	//appNamespace, _ := utils.AppNamespace(app, owner, workflowCfg.Namespace)
	name, _ := utils.FmtAppMgrName(app, owner, workflowCfg.Namespace)
	recommendMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", workflowCfg.Namespace, app),
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:      app,
			AppNamespace: workflowCfg.Namespace,
			AppOwner:     owner,
			Source:       insReq.Source.String(),
			Type:         v1alpha1.Recommend,
		},
	}
	a, err = client.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		a, err = client.AppV1alpha1().ApplicationManagers().Create(req.Request.Context(), recommendMgr, metav1.CreateOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	} else {
		patchData := map[string]interface{}{
			"spec": map[string]interface{}{
				"source": insReq.Source.String(),
			},
		}
		patchByte, err := json.Marshal(patchData)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		_, err = client.AppV1alpha1().ApplicationManagers().Patch(req.Request.Context(),
			a.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	}
	now := metav1.Now()
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:  v1alpha1.InstallOp,
		State:   v1alpha1.Installing,
		Message: "installing recommend",
		Payload: map[string]string{
			"version": workflowCfg.Cfg.Metadata.Version,
		},
		StatusTime: &now,
		UpdateTime: &now,
	}
	a, err = utils.UpdateAppMgrStatus(a.Name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	opRecord := v1alpha1.OpRecord{
		OpType:     v1alpha1.InstallOp,
		Version:    workflowCfg.Cfg.Metadata.Version,
		Source:     a.Spec.Source,
		Status:     v1alpha1.Completed,
		StatusTime: &now,
	}

	klog.Info("Start to install workflow, ", workflowCfg)
	err = workflowinstaller.Install(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		opRecord.Status = v1alpha1.Failed
		opRecord.Message = fmt.Sprintf(constants.OperationFailedTpl, a.Status.OpType, err.Error())
		e := utils.UpdateStatus(a, opRecord.Status, &opRecord, "", opRecord.Message)
		if e != nil {
			klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
		}
		api.HandleError(resp, req, err)
		return
	}

	now = metav1.Now()
	opRecord.Message = fmt.Sprintf(constants.InstallOperationCompletedTpl, a.Spec.Type.String(), a.Spec.AppName)
	err = utils.UpdateStatus(a, v1alpha1.Completed, &opRecord, "", opRecord.Message)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) cleanRecommendFeedData(name, owner string) error {
	knowledgeAPI := fmt.Sprintf("http://knowledge-base-api.user-system-%s:3010", owner)
	feedAPI := knowledgeAPI + "/knowledge/feed/algorithm/" + name

	client := resty.New()
	response, err := client.R().Get(feedAPI)
	if err != nil {
		return err
	}
	if response.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to get knowledge feed list status=%s body=%s", response.Status(), response.String())
		return errors.New(response.Status())
	}
	var ret workflowinstaller.KnowledgeAPIResp
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		return err
	}
	feedUrls := ret.Data
	klog.Info("Start to clean recommend feed data ", feedAPI, len(feedUrls))
	if len(feedUrls) > 0 {
		limit := 10
		removeClient := resty.New()
		for i := 0; i*limit < len(feedUrls); i++ {
			start := i * limit
			end := start + limit
			if end > len(feedUrls) {
				end = len(feedUrls)
			}
			removeList := feedUrls[start:end]
			reqData := workflowinstaller.KnowledgeFeedDelReq{FeedUrls: removeList}
			removeBody, _ := json.Marshal(reqData)
			res, _ := removeClient.SetTimeout(5*time.Second).R().SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
				SetBody(removeBody).Delete(feedAPI)

			if res.StatusCode() == http.StatusOK {
				klog.Info("Delete feed success: ", i, len(removeList))
			} else {
				klog.Errorf("Failed to clean recommend feed data err=%s", string(res.Body()))
			}
		}
	}
	klog.Info("Delete entry success page: ", name, len(feedUrls))
	return nil
}

type KnowledgeInstallMsg struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (h *Handler) notifyKnowledgeInstall(title, name, owner string) error {
	knowledgeAPI := "http://rss-svc.os-system:3010/knowledge/algorithm/recommend/install"
	klog.Info("Start to notify knowledge to Install ", knowledgeAPI, title, name)

	msg := KnowledgeInstallMsg{
		ID:    name,
		Title: title,
	}
	body, jsonErr := json.Marshal(msg)
	if jsonErr != nil {
		return jsonErr
	}
	resp, err := client.SetTimeout(10*time.Second).R().
		SetHeader("X-Bfl-User", owner).
		SetBody(body).Post(knowledgeAPI)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to notify knowledge to Install status=%s", entryResp.Status())
		return errors.New(resp.Status())
	}
	return nil
}

func (h *Handler) notifyKnowledgeUnInstall(name, owner string) error {
	knowledgeAPI := "http://rss-svc.os-system:3010/knowledge/algorithm/recommend/uninstall"

	msg := KnowledgeInstallMsg{
		ID: name,
	}
	body, jsonErr := json.Marshal(msg)
	if jsonErr != nil {
		return jsonErr
	}
	klog.Info("Start to notify knowledge to Install ", knowledgeAPI)
	resp, err := client.SetTimeout(10*time.Second).R().
		SetHeader("X-Bfl-User", owner).
		SetBody(body).Post(knowledgeAPI)

	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to notify knowledge to Install status=%s", resp.Status())
		return errors.New(resp.Status())
	}
	return nil
}
func (h *Handler) cleanRecommendEntryData(name, owner string) error {
	knowledgeAPI := fmt.Sprintf("http://knowledge-base-api.user-system-%s:3010", owner)
	entryAPI := knowledgeAPI + "/knowledge/entry/algorithm/" + name
	klog.Info("Start to clean recommend entry data ", entryAPI)
	client := resty.New().SetTimeout(10*time.Second).
		SetHeader("X-Bfl-User", owner)
	entryResp, err := client.R().Get(entryAPI)
	if err != nil {
		return err
	}
	if entryResp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to get knowledge entry list status=%s", entryResp.Status())
		return errors.New(entryResp.Status())
	}
	var ret workflowinstaller.KnowledgeAPIResp
	err = json.Unmarshal(entryResp.Body(), &ret)
	if err != nil {
		return err
	}
	urlsCount := len(ret.Data)
	if urlsCount > 0 {
		limit := 100
		removeClient := resty.New()
		entryRemoveAPI := knowledgeAPI + "/knowledge/entry/" + name
		for i := 0; i*limit < urlsCount; i++ {
			start := i * limit
			end := start + limit
			if end > urlsCount {
				end = urlsCount
			}
			removeList := ret.Data[start:end]
			removeBody, _ := json.Marshal(removeList)
			res, _ := removeClient.SetTimeout(5*time.Second).R().SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
				SetBody(removeBody).Delete(entryRemoveAPI)

			if res.StatusCode() == http.StatusOK {
				klog.Info("Delete entry success page: ", i, len(removeList))
			} else {
				klog.Info("Clean recommend entry data error:", string(removeBody), string(res.Body()))
			}
		}

	}
	klog.Info("Delete entry success page: ", name, urlsCount)
	return nil
}

func (h *Handler) uninstallRecommend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	var err error

	//namespace := fmt.Sprintf("%s-%s", app, owner)
	namespace, err := utils.AppNamespace(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	workflowCfg := &workflowinstaller.WorkflowConfig{
		WorkflowName: app,
		Namespace:    namespace,
		OwnerName:    owner,
	}
	klog.Infof("Start to uninstall workflow name=%s", workflowCfg.WorkflowName)

	go h.cleanRecommendEntryData(app, owner)
	go h.notifyKnowledgeUnInstall(app, owner)

	now := metav1.Now()
	var recommendMgr *v1alpha1.ApplicationManager
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.UninstallOp,
		State:      v1alpha1.Uninstalling,
		Message:    "try to uninstall a recommend",
		StatusTime: &now,
		UpdateTime: &now,
	}
	name, _ := utils.FmtAppMgrName(app, owner, namespace)
	recommendMgr, err = utils.UpdateAppMgrStatus(name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	defer func() {
		if err != nil {
			now := metav1.Now()
			message := fmt.Sprintf(constants.OperationFailedTpl, recommendMgr.Status.OpType, err.Error())
			opRecord := v1alpha1.OpRecord{
				OpType:     v1alpha1.UninstallOp,
				Message:    message,
				Source:     recommendMgr.Spec.Source,
				Version:    recommendMgr.Status.Payload["version"],
				Status:     v1alpha1.Failed,
				StatusTime: &now,
			}
			e := utils.UpdateStatus(recommendMgr, v1alpha1.Failed, &opRecord, "", message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status in uninstall Recommend name=%s err=%v", recommendMgr.Name, e)
			}
		}
	}()

	err = workflowinstaller.Uninstall(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	klog.Infof("Start to delete namespace=%s", namespace)
	err = client.KubeClient.Kubernetes().CoreV1().Namespaces().Delete(req.Request.Context(), namespace, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("Failed to delete workflow namespace=%s err=%v", namespace, err)
		api.HandleError(resp, req, err)
		return
	}
	go func() {
		timer := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-timer.C:
				_, err := client.KubeClient.Kubernetes().CoreV1().Namespaces().
					Get(context.TODO(), namespace, metav1.GetOptions{})
				if err != nil {
					if apierrors.IsNotFound(err) {
						now := metav1.Now()
						message := fmt.Sprintf(constants.UninstallOperationCompletedTpl, recommendMgr.Spec.Type.String(), recommendMgr.Spec.AppName)
						opRecord := v1alpha1.OpRecord{
							OpType:     v1alpha1.UninstallOp,
							Message:    message,
							Source:     recommendMgr.Spec.Source,
							Version:    recommendMgr.Status.Payload["version"],
							Status:     v1alpha1.Completed,
							StatusTime: &now,
						}
						err = utils.UpdateStatus(recommendMgr, opRecord.Status, &opRecord, "", message)
						if err != nil {
							klog.Errorf("Failed to update applicationmanager name=%s in uninstall Recommend err=%v", recommendMgr.Name, err)
						}
						return
					}

				}
			}
		}
	}()

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) upgradeRecommend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	var err error
	upReq := &api.UpgradeRequest{}
	err = req.ReadEntity(upReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	var recommendMgr *v1alpha1.ApplicationManager
	var workflowCfg *workflowinstaller.WorkflowConfig

	defer func() {
		now := metav1.Now()
		opRecord := v1alpha1.OpRecord{
			OpType:     v1alpha1.UpgradeOp,
			Message:    fmt.Sprintf(constants.UpgradeOperationCompletedTpl, recommendMgr.Spec.Type.String(), recommendMgr.Spec.AppName),
			Source:     recommendMgr.Spec.Source,
			Version:    workflowCfg.Cfg.Metadata.Version,
			Status:     v1alpha1.Completed,
			StatusTime: &now,
		}
		if err != nil {
			opRecord.Status = v1alpha1.Failed
			opRecord.Message = fmt.Sprintf(constants.OperationFailedTpl, recommendMgr.Status.OpType, err.Error())
		}
		e := utils.UpdateStatus(recommendMgr, opRecord.Status, &opRecord, "", opRecord.Message)
		if e != nil {
			klog.Errorf("Failed to update applicationmanager status in upgrade recommend name=%s err=%v", recommendMgr.Name, e)
		}

	}()

	now := metav1.Now()
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.UpgradeOp,
		State:      v1alpha1.Upgrading,
		Message:    "try to upgrade a recommend",
		StatusTime: &now,
		UpdateTime: &now,
	}

	klog.Infof("Download latest version chart and get workflow config name=%s repoURL=%s", app, upReq.RepoURL)
	workflowCfg, err = getWorkflowConfigFromRepo(req.Request.Context(), owner, app, upReq.RepoURL, "", token)
	if err != nil {
		klog.Errorf("Failed to get workflow config name=%s repoURL=%s err=%v, ", app, upReq.RepoURL, err)
		api.HandleError(resp, req, err)
		return
	}
	name, _ := utils.FmtAppMgrName(app, owner, workflowCfg.Namespace)
	recommendMgr, err = utils.UpdateAppMgrStatus(name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	klog.Infof("Start to upgrade workflow name=%s", workflowCfg.WorkflowName)
	err = workflowinstaller.Upgrade(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})

}
