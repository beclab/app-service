package apiserver

import (
	"net/http"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"

	restfulspec "github.com/emicklei/go-restful-openapi/v2"
	"github.com/emicklei/go-restful/v3"
)

const (
	ParamAppName        = "name"
	ParamAppNamespace   = "namespace"
	ParamInstallationID = "iuid"
	ParamUserName       = "user"
	ParamServiceName    = "service"
	ParamEntranceName   = "entrance_name"
	ParamModelID        = "model_id"

	ParamWorkflowName = "name"
	UserName          = "name"

	ParamDataType = "datatype"
	ParamGroup    = "group"
	ParamVersion  = "version"
)

var (
	MODULE_TAGS = []string{"app-service"}
)

func addServiceToContainer(c *restful.Container, handler *Handler) error {
	c.Filter(handler.createClientSet)
	c.Filter(handler.authenticate)

	ws := newWebService()

	// handler_service
	ws.Route(ws.GET("/applications/{"+ParamAppNamespace+"}/{"+ParamAppName+"}").
		To(handler.get).
		Doc("Get the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the namespace of a application")).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a application", nil))

	ws.Route(ws.GET("/applications").
		To(handler.list).
		Doc("List user's applications").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the list of user's application", []appv1alpha1.Application{}))

	ws.Route(ws.GET("/user-apps/{"+ParamUserName+"}").
		To(handler.listBackend).
		Doc("List user's applications from backend").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "Success to get the list of user's application", []appv1alpha1.Application{}))

	// handler_installer

	ws.Route(ws.POST("/application/deps").
		To(handler.checkDependencies).
		Reads(depRequest{}).
		Doc("check whether specified dependencies were meet").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "return not satisfied dependencies", api.DependenciesResp{}))

	ws.Route(ws.GET("/applications/{"+ParamAppName+"}/version").
		To(handler.releaseVersion).
		Doc("get application chart version").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "application chart version", &api.ReleaseVersionResponse{}))

	// handler_registry
	ws.Route(ws.GET("/registry/applications").
		To(handler.listRegistry).
		Doc("List charts registry applications (to be seperated)").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the list of the applications in registry", nil))

	ws.Route(ws.GET("/registry/applications/{"+ParamAppName+"}").
		To(handler.registryGet).
		Doc("get the application chart from registry (to be seperated)").
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the application in registry", nil))

	// handler_user
	ws.Route(ws.POST("/users").
		To(tryAppInstall(handler.createUser)).
		Doc("create new user's launcher and apps").
		Param(ws.PathParameter(ParamAppName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to create", nil))

	ws.Route(ws.DELETE("/users/{"+ParamUserName+"}").
		To(handler.deleteUser).
		Doc("delete a user's launcher and apps").
		Param(ws.PathParameter(ParamUserName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to delete", nil))

	ws.Route(ws.GET("/users/{"+ParamUserName+"}/status").
		To(handler.userStatus).
		Doc("get a user's launcher and apps creating or deleting status").
		Param(ws.PathParameter(ParamUserName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.GET("/users").
		To(handler.handleUsers).
		Doc("get a user's launcher and apps creating or deleting status").
		Param(ws.PathParameter(ParamUserName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.GET("/users/{"+ParamUserName+"}").
		To(handler.handleUser).
		Doc("get a user's launcher and apps creating or deleting status").
		Param(ws.PathParameter(ParamUserName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.POST("/users/{user}/limits").
		To(handler.handleUpdateUserLimits).
		Doc("update user limits").
		Param(ws.PathParameter(ParamUserName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "update success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.GET("/user-info").
		To(handler.userInfo).
		Doc("get a user's role").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.GET("/users/{"+ParamUserName+"}/metrics").
		To(handler.userMetrics).
		Doc("get a user's metric").
		Param(ws.PathParameter(ParamAppName, "the name of the user")).
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	//ws.Route(ws.GET("/users/{"+ParamUserName+"}/resource").
	//	To(handler.userResourceStatus).
	//	Doc("get a user's resource and resource usage").
	//	Param(ws.PathParameter(ParamAppName, "the name of the user")).
	//	Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
	//	Param(ws.HeaderParameter("X-Authorization", "Auth token")).
	//	Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.GET("/user/resource").
		To(handler.curUserResource).
		Doc("get a cur user's resource and resource usage").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	ws.Route(ws.GET("/cluster/resource").
		To(handler.clusterResource).
		Doc("get cluster resource and resource usage").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get", nil))

	// handler_system
	ws.Route(ws.POST("/system/service/enable/sync").
		To(handler.enableServiceSync).
		Doc("enable user's system service 'Sync' ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to enable", nil))

	ws.Route(ws.POST("/system/service/disable/sync").
		To(handler.disableServiceSync).
		Doc("disable user's system service 'Sync' ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to disable", nil))

	ws.Route(ws.POST("/system/service/enable/backup").
		To(handler.enableServiceBackup).
		Doc("enable user's system service 'Backup' ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to enable", nil))

	ws.Route(ws.POST("/system/service/disable/backup").
		To(handler.disableServiceBackup).
		Doc("disable user's system service 'Backup' ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to disable", nil))

	// handler_settings
	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/setup").
		To(tryAppInstall(handler.setupApp)).
		Doc("update the application settings").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Consumes(restful.MIME_JSON).
		Returns(http.StatusOK, "Success to update the application settings", nil))

	ws.Route(ws.GET("/applications/{"+ParamAppName+"}/setup").
		To(handler.getAppSettings).
		Doc("get the application settings").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the application settings", nil))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/{"+ParamEntranceName+"}/setup").
		To(tryAppInstall(handler.setupAppEntranceDomain)).
		Doc("update the application settings of custom domain").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.PathParameter(ParamEntranceName, "the name of a application entrance")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Consumes(restful.MIME_JSON).
		Returns(http.StatusOK, "Success to update the application settings of domain", nil))

	ws.Route(ws.GET("/applications/{"+ParamAppName+"}/{"+ParamEntranceName+"}/setup").
		To(handler.getAppEntrancesSettings).
		Doc("get the application settings").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.PathParameter(ParamEntranceName, "the name of a application entrance")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to update the application settings", nil))

	ws.Route(ws.GET("/applications/{"+ParamAppName+"}/entrances").
		To(handler.getAppEntrances).
		Doc("get the application entrances").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the application entrances", nil))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/{"+ParamEntranceName+"}/auth-level").
		To(tryAppInstall(handler.setupAppAuthLevel)).
		Doc("set the entrance auth level").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.PathParameter(ParamEntranceName, "the name of a application entrance")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to set the application entrance auth level", nil))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/{"+ParamEntranceName+"}/policy").
		To(tryAppInstall(handler.setupAppEntrancePolicy)).
		Doc("set the entrance policy").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.PathParameter(ParamEntranceName, "the name of a application entrance")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to set the application entrance policy", nil))

	ws.Route(ws.POST("/gpu/disable/managed-memory").
		To(handler.disableGpuManagedMemory).
		Doc("disable nvshare's managed memory ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to disable", nil))

	ws.Route(ws.POST("/gpu/enable/managed-memory").
		To(handler.enableGpuManagedMemory).
		Doc("enable nvshare's managed memory ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to enable", nil))

	ws.Route(ws.GET("/gpu/managed-memory").
		To(handler.getManagedMemoryValue).
		Doc("get nvshare's managed memory enabled or not").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get ", &ResultResponse{}))

	// handler_upgrade
	ws.Route(ws.GET("/upgrade/newversion").
		To(handler.newVersion).
		Doc("get there is a new version can be upgrade or not").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the new version", &ResultResponse{}))

	ws.Route(ws.GET("/upgrade/state").
		To(handler.upgradeState).
		Doc("get the running upgrade state").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get state", &ResultResponse{}))

	ws.Route(ws.POST("/upgrade").
		To(tryAppInstall(requireAdmin(handler, handler.upgrade))).
		Doc("upgrade system").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to start upgrading", &ResultResponse{}))

	ws.Route(ws.POST("/upgrade/cancel").
		To(requireAdmin(handler, handler.upgradeCancel)).
		Doc("cancel the running upgrading").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to cancel", &ResultResponse{}))

	// handler_webhook
	ws.Route(ws.POST("/sandbox/inject").
		To(handler.sandboxInject).
		Doc("mutating webhook for sandbox sidecar injection ").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "Success to inject", nil)).
		Consumes(restful.MIME_JSON)

	// handler application namespace validate
	ws.Route(ws.POST("/appns/validate").
		To(handler.appNamespaceValidate).
		Doc("validating webhook for validate app install namespace").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "App namespace validated success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/runasuser/inject").
		To(handler.handleRunAsUser).
		Doc("mutating webhook for inject runasuser 1000 for third party app's pod").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "inject runasuser success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/workflow/inject").
		To(handler.cronWorkflowInject).
		Doc("mutating webhook for cron workflow").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "cron workflow inject success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/gpulimit/inject").
		To(handler.gpuLimitInject).
		Doc("add resources limits for deployment/statefulset").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "add limit success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/app-label/inject").
		To(handler.appLabelInject).
		Doc("add resources limits for deployment/statefulset").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "add limit success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/provider-registry/validate").
		To(handler.providerRegistryValidate).
		Doc("validating webhook for validate app install namespace").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "provider registry validated success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/user/validate").
		To(handler.userValidate).
		Doc("validating webhook for validate user creation").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "user validated success", nil)).
		Consumes(restful.MIME_JSON)

	ws.Route(ws.POST("/metrics/highload").
		To(handler.highload).
		Doc("Provide system resources high load event to callback").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "Success", nil))

	ws.Route(ws.POST("/metrics/user/highload").
		To(handler.userHighLoad).
		Doc("Provide user resources high load event to callback").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "Success", nil))

	// app operate
	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/install").
		To(tryAppInstall(handler.install)).
		Doc("Install the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to begin a installation of the application", &api.InstallationResponse{}))

	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/uninstall").
		To(tryAppInstall(handler.uninstall)).
		Doc("Uninstall the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to begin a uninstallation of the application", &api.InstallationResponse{}))

	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/suspend").
		To(tryAppInstall(handler.suspend)).
		Doc("suspend the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to suspend of the application", &api.InstallationResponseData{}))

	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/resume").
		To(tryAppInstall(handler.resume)).
		Doc("resume the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to begin to resume the application", &api.InstallationResponseData{}))

	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/upgrade").
		To(tryAppInstall(handler.appUpgrade)).
		Reads(api.UpgradeRequest{}).
		Doc("Upgrade the application").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to begin upgrade of the application", &api.ReleaseUpgradeResponse{}))

	ws.Route(ws.POST("/apps/{"+ParamAppName+"}/cancel").
		To(handler.cancel).
		Doc("cancel pending or installing app").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamInstallationID, "the id of a installation or uninstallation")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a installation or uninstallation status", &api.InstallationResponse{}))

	ws.Route(ws.GET("/apps/{"+ParamAppName+"}/status").
		To(handler.status).
		Doc("get specified app status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a app status", nil))

	ws.Route(ws.GET("/apps/status").
		To(handler.appsStatus).
		Doc("get specified app status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a apps status", nil))

	ws.Route(ws.GET("/apps/{"+ParamAppName+"}/operate").
		To(handler.operate).
		Doc("get specified app status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a apps status", nil))

	ws.Route(ws.GET("/apps/operate").
		To(handler.appsOperate).
		Doc("get specified app status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a apps status", nil))

	ws.Route(ws.GET("/apps/{"+ParamAppName+"}/operate_history").
		To(handler.operateHistory).
		Doc("get specified app operate history").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a apps status", nil))

	ws.Route(ws.GET("/apps/operate_history").
		To(handler.allOperateHistory).
		Doc("get specified all app operate history").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a apps operate history", nil))

	ws.Route(ws.GET("/apps").
		To(handler.apps).
		Doc("get list of app").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get list of app", nil))

	ws.Route(ws.GET("/all/apps").
		To(handler.allUsersApps).
		Doc("get list of app for all user").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get list of app for all user", nil))

	ws.Route(ws.GET("/apps/{"+ParamAppName+"}").
		To(handler.getApp).
		Doc("get an app").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get an app", nil))

	ws.Route(ws.GET("/apps/oamvalues").
		To(handler.oamValues).
		Doc("get an app oam values").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get an app oamvalues", nil))

	ws.Route(ws.POST("/apps/image-info").
		To(handler.imageInfo).
		Doc("get an app image info").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get an app image info", nil))

	ws.Route(ws.GET("/perms").
		To(handler.applicationPermissionList).
		Doc("get app permissions list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get an apps permissions list", nil))

	ws.Route(ws.GET("/perms/{"+ParamAppName+"}").
		To(handler.getApplicationPermission).
		Doc("get an app permission").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamAppName, "the name of application")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get an app permission", nil))

	ws.Route(ws.GET("/perms/provider-registry/{"+ParamDataType+"}/{"+ParamGroup+"}/{"+ParamVersion+"}").
		To(handler.getProviderRegistry).
		Doc("get an app permission").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Param(ws.PathParameter(ParamDataType, "the dataType of providerregistry")).
		Param(ws.PathParameter(ParamGroup, "the group of providerregistry")).
		Param(ws.PathParameter(ParamVersion, "the version of providerregistry")).
		Returns(http.StatusOK, "success to get an providerregistry", nil))

	ws.Route(ws.GET("/apps/provider-registry/{"+ParamAppName+"}").
		To(handler.getApplicationProviderList).
		Doc("get an app provider list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Param(ws.PathParameter(ParamAppName, "the appName of providerregistry")).
		Returns(http.StatusOK, "success to get an app providerregistry list", nil))

	ws.Route(ws.GET("/apps/{"+ParamAppName+"}/subject").
		To(handler.getApplicationSubject).
		Doc("get an app subject").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Param(ws.PathParameter(ParamAppName, "the name of app")).
		Returns(http.StatusOK, "success to get an app subject", nil))

	ws.Route(ws.GET("/apps/pending-installing/task").
		To(handler.pendingOrInstallingApps).
		Doc("get list of pending or installing app").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "success to get list of app", nil))

	ws.Route(ws.GET("/terminus/version").
		To(handler.terminusVersion).
		Doc("get version of terminus").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "get version of terminus", nil))

	ws.Route(ws.GET("/terminus/nodes").
		To(handler.nodes).
		Doc("get terminus all nodes").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "get nodes of terminus", nil))

	ws.Route(ws.POST("/recommends/{"+ParamWorkflowName+"}/install").
		To(handler.installRecommend).
		Doc("Install the recommend workflow").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a workflow")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to install the workflow", &api.InstallationResponse{}))

	ws.Route(ws.POST("/recommends/{"+ParamWorkflowName+"}/uninstall").
		To(handler.uninstallRecommend).
		Doc("Uninstall the recommend workflow").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to uninstall the recommend", &api.InstallationResponse{}))

	ws.Route(ws.POST("/recommends/{"+ParamWorkflowName+"}/upgrade").
		To(handler.upgradeRecommend).
		Doc("upgrade the recommend workflow").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to upgrade the recommend", &api.InstallationResponse{}))

	ws.Route(ws.GET("/recommends/{"+ParamWorkflowName+"}/status").
		To(handler.statusRecommend).
		Doc("get the recommend workflow status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the recommend status", &api.InstallationResponse{}))

	ws.Route(ws.GET("/recommends/status").
		To(handler.statusRecommendList).
		Doc("get the recommend workflow status list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the recommend status list", &api.InstallationResponse{}))

	ws.Route(ws.GET("/recommenddev/{"+UserName+"}/status").
		To(handler.statusListDev).
		Doc("get the recommend workflow status list dev").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Returns(http.StatusOK, "Success to get the recommend status list", &api.InstallationResponse{}))

	ws.Route(ws.GET("/recommends/{"+ParamWorkflowName+"}/operate").
		To(handler.operateRecommend).
		Doc("get specified recommend operate").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a workflow operate", nil))

	ws.Route(ws.GET("/recommends/operate").
		To(handler.operateRecommendList).
		Doc("get recommends operate list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "get recommends operate list", nil))

	ws.Route(ws.GET("/recommends/{"+ParamWorkflowName+"}/operate_history").
		To(handler.operateRecommendHistory).
		Doc("get specified recommend operate history").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a recommend status", nil))

	ws.Route(ws.GET("/recommends/operate_history").
		To(handler.allOperateRecommendHistory).
		Doc("get specified all recommend operate history").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "get specified all recommend operate history", nil))

	// middleware route
	ws.Route(ws.POST("/middlewares/{"+ParamAppName+"}/install").
		To(handler.installMiddleware).
		Doc("Install the middleware").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a middleware")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to install the middleware", &api.InstallationResponse{}))

	ws.Route(ws.POST("/middlewares/{"+ParamAppName+"}/uninstall").
		To(handler.uninstallMiddleware).
		Doc("Uninstall the middleware").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a recommend")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to uninstall the middleware", &api.InstallationResponse{}))

	ws.Route(ws.GET("/middlewares/{"+ParamAppName+"}/status").
		To(handler.statusMiddleware).
		Doc("get the middleware status").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of a middleware")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the middleware status", &api.InstallationResponse{}))

	ws.Route(ws.GET("/middlewares/status").
		To(handler.statusMiddlewareList).
		Doc("get the middleware status list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get the recommend status list", &api.InstallationResponse{}))

	ws.Route(ws.GET("/middlewares/{"+ParamAppName+"}/operate").
		To(handler.operateMiddleware).
		Doc("get specified middleware operate").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamWorkflowName, "the name of middleware")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get a middleware operate", nil))

	ws.Route(ws.GET("/middlewares/operate").
		To(handler.operateMiddlewareList).
		Doc("get middlewares operate list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "get middleware operate list", nil))

	ws.Route(ws.POST("/middlewares/{"+ParamAppName+"}/cancel").
		To(handler.cancelMiddleware).
		Doc("cancel installing middleware").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.PathParameter(ParamInstallationID, "the id of a installation or uninstallation")).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to cancel app install", &api.InstallationResponse{}))

	ws.Route(ws.POST("/apps/manifest/render").
		To(handler.renderManifest).
		Doc("render olares manifest").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to render olares manifest", &api.ManifestRenderResponse{}))

	ws.Route(ws.GET("/users/admin/username").
		To(handler.adminUsername).
		Doc("return admin username").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get admin username", nil))

	ws.Route(ws.GET("/users/admins").
		To(handler.adminUserList).
		Doc("return admin list").
		Metadata(restfulspec.KeyOpenAPITags, MODULE_TAGS).
		Param(ws.HeaderParameter("X-Authorization", "Auth token")).
		Returns(http.StatusOK, "Success to get admin username", nil))

	c.Add(ws)

	return nil
}
