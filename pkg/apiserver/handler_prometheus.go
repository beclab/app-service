package apiserver

import (
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/prometheus"

	"github.com/emicklei/go-restful/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	metrics, supportArch, err := apputils.GetClusterResource(token)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	type App struct {
		Title   string `json:"title"`
		State   string `json:"state"`
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
		s, err := getMiddlewareStatus(req.Request.Context(), h.kubeConfig, am.Spec.AppName, am.Spec.AppOwner)
		if err != nil && !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		apps = append(apps, App{
			Title:   s.Title,
			Name:    am.Spec.AppName,
			Type:    am.Spec.Type.String(),
			Version: am.Status.Payload["version"],
			State:   s.ResourceStatus,
		})

	}

	var appList v1alpha1.ApplicationList
	err = h.ctrlClient.List(req.Request.Context(), &appList)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, a := range appList.Items {
		//if a.Status.State != v1alpha1.AppRunning.String() {
		//	continue
		//}
		if a.Spec.Settings["clusterScoped"] != "true" {
			continue
		}
		title := ""
		for i := range a.Spec.Entrances {
			if !a.Spec.Entrances[i].Invisible {
				title = a.Spec.Entrances[i].Title
				break
			}
		}
		apps = append(apps, App{
			Title:   title,
			Name:    a.Spec.Name,
			Type:    "app",
			Version: a.Spec.Settings["version"],
			State:   a.Status.State,
		})
	}

	resp.WriteAsJson(map[string]interface{}{
		"metrics": metrics,
		"nodes":   supportArch,
		"apps":    apps,
	})
}
