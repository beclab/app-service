package apiserver

import (
	"time"

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
	resp.WriteAsJson(map[string]interface{}{
		"metrics": metrics,
		"nodes":   supportArch,
	})
}
