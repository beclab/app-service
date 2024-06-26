package apiserver

import (
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/prometheus"

	"github.com/emicklei/go-restful/v3"
)

func (h *Handler) userMetrics(req *restful.Request, resp *restful.Response) {
	username := req.PathParameter(ParamUserName)
	opts := prometheus.QueryOptions{
		Level:    prometheus.LevelUser,
		UserName: username,
	}
	p, err := prometheus.New(prometheus.Endpoint)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	metrics := p.GetNamedMetrics(req.Request.Context(), []string{"user_cpu_usage", "user_memory_usage"}, time.Now(), opts)
	resp.WriteAsJson(map[string]interface{}{"results": metrics})
}

func (h *Handler) clusterResource(req *restful.Request, resp *restful.Response) {
	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	metrics, supportArch, err := GetClusterResource(h.kubeConfig, token)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	type App struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Version string `json:"version"`
	}
	apps := make([]App, 0)
	var ams v1alpha1.ApplicationManagerList
	err = h.ctrlClient.List(req.Request.Context(), &ams)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	for _, am := range ams.Items {
		if am.Spec.Type != v1alpha1.Middleware {
			continue
		}
		if am.Status.State == v1alpha1.Completed && am.Status.OpType == v1alpha1.InstallOp {
			apps = append(apps, App{
				Name:    am.Spec.AppName,
				Type:    am.Spec.Type.String(),
				Version: am.Status.Payload["version"],
			})
		}

	}

	var appList v1alpha1.ApplicationList
	err = h.ctrlClient.List(req.Request.Context(), &appList)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, a := range appList.Items {
		if a.Status.State != v1alpha1.AppRunning.String() {
			continue
		}
		if a.Spec.Settings["clusterScoped"] != "true" {
			continue
		}
		apps = append(apps, App{
			Name:    a.Spec.Name,
			Type:    "app",
			Version: a.Spec.Settings["version"],
		})
	}

	resp.WriteAsJson(map[string]interface{}{
		"metrics": metrics,
		"nodes":   supportArch,
		"apps":    apps,
	})
}
