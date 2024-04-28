package apiserver

import (
	"errors"

	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/prometheus"
	"bytetrade.io/web3os/app-service/pkg/users/userspace/v1"

	"github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func (h *Handler) userAppsCreate(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet) // session client
	userName := req.PathParameter(ParamUserName)

	_, err := h.userspaceManager.CreateUserApps(client, h.kubeConfig, userName)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.UserAppsResponse{
		Response: api.Response{Code: 200},
		Data: api.UserAppsStatusRespData{
			User:   userName,
			Status: userspace.UserCreating,
		},
	})
}

func (h *Handler) userAppsDelete(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet) // session client
	userName := req.PathParameter(ParamUserName)

	_, err := h.userspaceManager.DeleteUserApps(client, h.kubeConfig, userName)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.UserAppsResponse{
		Response: api.Response{Code: 200},
		Data: api.UserAppsStatusRespData{
			User:   userName,
			Status: userspace.UserDeleting,
		},
	})
}

func (h *Handler) userAppsStatus(req *restful.Request, resp *restful.Response) {
	userName := req.PathParameter(ParamUserName)

	res := h.userspaceManager.TaskStatus(userName)

	if res == nil {
		api.HandleNotFound(resp, req, errors.New("user's task not found"))
		return
	}

	response := api.UserAppsResponse{
		Response: api.Response{Code: 200},
		Data: api.UserAppsStatusRespData{
			User:   userName,
			Status: res.Status,
		},
	}

	if res.Error != nil {
		response.Data.Status = userspace.UserFailed
		response.Data.Error = res.Error.Error()
	} else if res.Status == userspace.UserCreated {
		ports := res.Values.([]int32)
		response.Data.Ports.Desktop = ports[0]
		response.Data.Ports.Wizard = ports[1]
	}

	resp.WriteEntity(response)
}

func (h *Handler) userResourceStatus(req *restful.Request, resp *restful.Response) {
	username := req.PathParameter(ParamUserName)
	metrics, err := prometheus.GetCurUserResource(req.Request.Context(), h.kubeConfig, username)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(metrics)
}

func (h *Handler) curUserResource(req *restful.Request, resp *restful.Response) {
	username := req.Attribute(constants.UserContextAttribute).(string)
	metrics, err := prometheus.GetCurUserResource(req.Request.Context(), h.kubeConfig, username)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(metrics)
}

func (h *Handler) userInfo(req *restful.Request, resp *restful.Response) {
	username := req.Attribute(constants.UserContextAttribute).(string)
	gvr := schema.GroupVersionResource{
		Group:    "iam.kubesphere.io",
		Version:  "v1alpha2",
		Resource: "users",
	}
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	user, err := client.Resource(gvr).Get(req.Request.Context(), username, metav1.GetOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	annotations := user.GetAnnotations()
	role := annotations["bytetrade.io/owner-role"]
	userInfo := map[string]string{
		"username": user.GetName(),
		"role":     role,
	}

	resp.WriteAsJson(userInfo)
}
