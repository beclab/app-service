package apiserver

import (
	"fmt"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"github.com/emicklei/go-restful/v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (h *Handler) getAppEnv(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	owner := getCurrentUser(req)

	if appName == "" || owner == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("app name and owner are required"))
		return
	}

	appNamespace, err := utils.AppNamespace(appName, owner, "")
	if err != nil {
		api.HandleInternalError(resp, req, fmt.Errorf("failed to get app namespace: %v", err))
		return
	}

	envs := make([]sysv1alpha1.AppEnvVar, 0)
	var appEnv sysv1alpha1.AppEnv
	if err := client.IgnoreNotFound(h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Namespace: appNamespace, Name: apputils.FormatAppEnvName(appName, owner)}, &appEnv)); err != nil {
		api.HandleInternalError(resp, req, err)
		return
	}
	if len(appEnv.Envs) > 0 {
		envs = appEnv.Envs
	}

	resp.WriteAsJson(envs)
}

func (h *Handler) updateAppEnv(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	owner := getCurrentUser(req)

	if appName == "" || owner == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("app name and owner are required"))
		return
	}

	var updatedEnvs []sysv1alpha1.AppEnvVar
	if err := req.ReadEntity(&updatedEnvs); err != nil {
		api.HandleBadRequest(resp, req, fmt.Errorf("failed to parse request body: %v", err))
		return
	}

	appNamespace, err := utils.AppNamespace(appName, owner, "")
	if err != nil {
		api.HandleInternalError(resp, req, fmt.Errorf("failed to get app namespace: %v", err))
		return
	}

	var targetAppEnv sysv1alpha1.AppEnv
	if err := h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Namespace: appNamespace, Name: apputils.FormatAppEnvName(appName, owner)}, &targetAppEnv); err != nil {
		api.HandleInternalError(resp, req, err)
		return
	}

	updated := false
	original := targetAppEnv.DeepCopy()
	for i, existingEnv := range targetAppEnv.Envs {
		for _, env := range updatedEnvs {
			if existingEnv.EnvName == env.EnvName {
				if !existingEnv.Editable {
					api.HandleBadRequest(resp, req, fmt.Errorf("app env '%s' is not editable", env.EnvName))
					return
				}
				if existingEnv.Required && existingEnv.Default == "" && env.Value == "" {
					api.HandleBadRequest(resp, req, fmt.Errorf("app env '%s' is required", env.EnvName))
					return
				}
				if existingEnv.Value != env.Value {
					err := utils.CheckEnvValueByType(env.Value, existingEnv.Type)
					if err != nil {
						api.HandleBadRequest(resp, req, fmt.Errorf("failed to update app env '%s': %v", env.EnvName, err))
						return
					}
					targetAppEnv.Envs[i].Value = env.Value
					updated = true
					if existingEnv.ApplyOnChange {
						targetAppEnv.NeedApply = true
					}
				}
				break
			}
		}
	}

	if updated {
		if err := h.ctrlClient.Patch(req.Request.Context(), &targetAppEnv, client.MergeFrom(original)); err != nil {
			api.HandleInternalError(resp, req, err)
			return
		}
	}

	resp.WriteAsJson(targetAppEnv.Envs)
}
