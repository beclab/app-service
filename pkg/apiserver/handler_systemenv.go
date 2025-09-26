package apiserver

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type SystemEnvUpdateRequest struct {
	Value string `json:"value"`
}

// SystemEnvDetail extends SystemEnv with reference information
type SystemEnvDetail struct {
	sysv1alpha1.EnvVarSpec `json:",inline"`
	ReferencedBy           []AppEnvReferrer `json:"referencedBy"`
}

type AppEnvReferrer struct {
	AppName   string `json:"appName"`
	AppOwner  string `json:"appOwner"`
	Namespace string `json:"namespace,omitempty"`
}

func (h *Handler) ensureAdmin(req *restful.Request, resp *restful.Response) (string, bool) {
	owner := getCurrentUser(req)
	isAdmin, err := kubesphere.IsAdmin(req.Request.Context(), h.kubeConfig, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return "", false
	}
	if !isAdmin {
		api.HandleBadRequest(resp, req, fmt.Errorf("only admin user can operate systemenvs"))
		return "", false
	}
	return owner, true
}

// todo: is it allowed?
func (h *Handler) createSystemEnv(req *restful.Request, resp *restful.Response) {
	_, ok := h.ensureAdmin(req, resp)
	if !ok {
		return
	}
	var body sysv1alpha1.EnvVarSpec
	if err := req.ReadEntity(&body); err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}
	if body.EnvName == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("name is required"))
		return
	}

	err := utils.CheckEnvValueByType(body.Value, body.Type)
	if err != nil {
		api.HandleBadRequest(resp, req, fmt.Errorf("env value: %v", err))
		return
	}
	err = utils.CheckEnvValueByType(body.Default, body.Type)
	if err != nil {
		api.HandleBadRequest(resp, req, fmt.Errorf("env default value: %v", err))
		return
	}

	// validate and normalize resource name
	resourceName, err := utils.EnvNameToResourceName(body.EnvName)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	obj := &sysv1alpha1.SystemEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		EnvVarSpec: body,
	}

	if err := h.ctrlClient.Create(req.Request.Context(), obj); err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(obj.EnvVarSpec)
}

func (h *Handler) updateSystemEnv(req *restful.Request, resp *restful.Response) {
	_, ok := h.ensureAdmin(req, resp)
	if !ok {
		return
	}

	name := req.PathParameter(ParamEnvName)
	if name == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv name is required"))
		return
	}

	var body SystemEnvUpdateRequest
	if err := req.ReadEntity(&body); err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	ctx := req.Request.Context()
	// validate and normalize resource name from path
	resourceName, err := utils.EnvNameToResourceName(name)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}
	var current sysv1alpha1.SystemEnv
	if err := h.ctrlClient.Get(ctx, types.NamespacedName{Name: resourceName}, &current); err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if !current.Editable {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv '%s' is not editable", current.EnvName))
		return
	}

	if current.Required && current.Default == "" && body.Value == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv '%s' is required", current.EnvName))
		return
	}

	if current.Value != body.Value {
		err := utils.CheckEnvValueByType(body.Value, current.Type)
		if err != nil {
			api.HandleBadRequest(resp, req, err)
			return
		}
		klog.Infof("Updating SystemEnv %s value from '%s' to '%s'", resourceName, current.Value, body.Value)
		original := current.DeepCopy()
		current.Value = body.Value
		if err := h.ctrlClient.Patch(ctx, &current, client.MergeFrom(original)); err != nil {
			api.HandleError(resp, req, err)
			return
		}
	}

	resp.WriteAsJson(current.EnvVarSpec)
}

func (h *Handler) deleteSystemEnv(req *restful.Request, resp *restful.Response) {
	_, ok := h.ensureAdmin(req, resp)
	if !ok {
		return
	}

	name := req.PathParameter(ParamEnvName)
	if name == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv name is required"))
		return
	}

	ctx := req.Request.Context()
	resourceName, err := utils.EnvNameToResourceName(name)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}
	var current sysv1alpha1.SystemEnv
	if err := h.ctrlClient.Get(ctx, types.NamespacedName{Name: resourceName}, &current); err != nil {
		if apierrors.IsNotFound(err) {
			resp.WriteEntity(api.Response{Code: 200})
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	if current.Required {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv '%s' is required", current.EnvName))
		return
	}

	if err := h.ctrlClient.Delete(ctx, &current); err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
	}

	resp.WriteEntity(api.Response{Code: 200})
}

// listSystemEnvs returns all system env specs
func (h *Handler) listSystemEnvs(req *restful.Request, resp *restful.Response) {
	var list sysv1alpha1.SystemEnvList
	if err := h.ctrlClient.List(req.Request.Context(), &list); err != nil {
		api.HandleError(resp, req, err)
		return
	}

	result := make([]sysv1alpha1.EnvVarSpec, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, item.EnvVarSpec)
	}
	resp.WriteAsJson(result)
}

// getSystemEnvDetail returns a system env spec along with referencing app envs
func (h *Handler) getSystemEnvDetail(req *restful.Request, resp *restful.Response) {
	_, ok := h.ensureAdmin(req, resp)
	if !ok {
		return
	}

	name := req.PathParameter(ParamEnvName)
	if name == "" {
		api.HandleBadRequest(resp, req, fmt.Errorf("systemenv name is required"))
		return
	}

	ctx := req.Request.Context()
	resourceName, err := utils.EnvNameToResourceName(name)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	var current sysv1alpha1.SystemEnv
	if err := h.ctrlClient.Get(ctx, types.NamespacedName{Name: resourceName}, &current); err != nil {
		api.HandleError(resp, req, err)
		return
	}

	detail := SystemEnvDetail{EnvVarSpec: current.EnvVarSpec}

	var appEnvList sysv1alpha1.AppEnvList
	if err := h.ctrlClient.List(ctx, &appEnvList); err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, ae := range appEnvList.Items {
		for _, envVar := range ae.Envs {
			if envVar.ValueFrom != nil && envVar.ValueFrom.EnvName == current.EnvName {
				detail.ReferencedBy = append(detail.ReferencedBy, AppEnvReferrer{
					AppName:   ae.AppName,
					AppOwner:  ae.AppOwner,
					Namespace: ae.Namespace,
				})
				break
			}
		}
	}

	resp.WriteAsJson(detail)
}
