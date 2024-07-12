package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/provider"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/webhook"

	"github.com/emicklei/go-restful/v3"
	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errNilAdmissionRequest = fmt.Errorf("nil admission request")
)

func (h *Handler) sandboxInject(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received mutating webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionRequestBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}

	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionRequestBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decoding admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError(err)
	} else {
		admissionResp.Response = h.mutate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}

	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}

	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Errorf("Failed to write response admin review namespace=%s err=%v", requestForNamespace, err)
		return
	}

	klog.Errorf("Done responding to admission request for pod with UUID=%s namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) mutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Errorf("Failed to get admission request, err=admission request is nil")
		return h.sidecarWebhook.AdmissionError(errNilAdmissionRequest)
	}
	var err error
	// Decode the Pod spec from the request
	var pod corev1.Pod
	if err = json.Unmarshal(req.Object.Raw, &pod); err != nil {
		klog.Errorf("Failed to unmarshal admission request object raw to pod with UUID=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(err)
	}

	// Start building the response
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	if pod.Spec.HostNetwork && !strings.HasPrefix(req.Namespace, "user-space-") {
		klog.Errorf("Pod with uid=%s namespace=%s has HostNetwork enabled, that's DENIED", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(errors.New("HostNetwork Enabled Unsupported"))
	}
	var injectPolicy, injectWs, injectUpload bool
	var perms []appinstaller.SysDataPermission
	if injectPolicy, injectWs, injectUpload, perms, err = h.sidecarWebhook.MustInject(ctx, &pod, req.Namespace); err != nil {
		return h.sidecarWebhook.AdmissionError(err)
	}
	klog.Infof("injectPolicy=%v, injectWs=%v, injectUpload=%v, perms=%v", injectPolicy, injectWs, injectUpload, perms)
	if !injectPolicy && !injectWs && !injectUpload && len(perms) == 0 {
		klog.Infof("Skipping sidecar injection for pod with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return resp
	}

	patchBytes, err := h.sidecarWebhook.CreatePatch(ctx, &pod, req, proxyUUID, injectPolicy, injectWs, injectUpload, perms)
	if err != nil {
		klog.Errorf("Failed to create patch for pod uuid=%s name=%s namespace=%s err=%v", proxyUUID, pod.Name, req.Namespace, err)
		return h.sidecarWebhook.AdmissionError(err)
	}

	h.sidecarWebhook.PatchAdmissionResponse(resp, patchBytes)
	klog.Infof("Success to create patch admission response for pod with uuid=%s namespace=%s", proxyUUID, req.Namespace)

	return resp
}

func (h *Handler) appNamespaceValidate(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received validate webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionReqBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}
	//owner := req.Attribute(constants.UserContextAttribute)
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionReqBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decode admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError(err)
	} else {
		admissionResp.Response = h.validate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}
	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Errorf("Failed to write response validate review in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Errorf("Done responding to admission[validate app namespace] request with uuid=%s namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) validate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission request err=admission request is nil")
		return h.sidecarWebhook.AdmissionError(errNilAdmissionRequest)
	}
	klog.Infof("Enter validate logic namespace=%s name=%s, kind=%s", req.Namespace, req.Name, req.Kind.Kind)
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	// fast path to return if req.Namespace is not in private namespaces.
	if !isInPrivateNamespace(req.Namespace) {
		klog.Infof("Skip validate namespace=%s", req.Namespace)
		return resp
	}

	// Decode the Object spec from the request.
	object := struct {
		metav1.ObjectMeta `json:"metadata,omitempty"`
	}{}
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &object)
	if err != nil {
		klog.Errorf("Failed to unmarshal request object raw with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(err)
	}

	if userspace.IsGeneratedApp(object.GetName()) {
		klog.Infof("Generated deployment validated success")
		return resp
	}

	labels := object.GetLabels()
	author := labels[constants.ApplicationAuthorLabel]
	if author != constants.ByteTradeAuthor {
		resp.Allowed = false
		klog.Errorf("You don't have permission to deploy with UID=%s in protected namespace, that's DENIED", object.GetUID())
		resp.Result = &metav1.Status{Message: fmt.Sprintf("You don't have permission to deploy in namespace=%s", object.Namespace)}
		return resp
	}
	klog.Infof("Done validate with UID=%s in protected namespace, that's APPROVE", object.GetUID())
	return resp
}

func (h *Handler) gpuLimitInject(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received mutating webhook[gpu-limit inject] request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionRequestBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionRequestBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decode admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError(err)
	} else {
		admissionResp.Response = h.gpuLimitMutate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}
	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Error("Failed to write response[gpu-limit inject] admin review in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done[gpu-limit inject] with uuid=%s in namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) gpuLimitMutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission Request, err=admission request is nil")
		return h.sidecarWebhook.AdmissionError(errNilAdmissionRequest)
	}
	klog.Infof("Enter gpuLimitMutate namespace=%s name=%s kind=%s", req.Namespace, req.Name, req.Kind.Kind)

	object := struct {
		metav1.ObjectMeta `json:"metadata,omitempty"`
	}{}
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &object)
	if err != nil {
		klog.Errorf("Error unmarshalling request with UUID %s in namespace %s, error %v ", proxyUUID, req.Namespace, err)
		return h.sidecarWebhook.AdmissionError(err)
	}

	var tpl *corev1.PodTemplateSpec

	switch req.Kind.Kind {
	case "Deployment":
		var d *appsv1.Deployment
		if err = json.Unmarshal(req.Object.Raw, &d); err != nil {
			klog.Errorf("Error unmarshaling request with UUID %s in namespace %s, %v", proxyUUID, req.Namespace, err)
			return h.sidecarWebhook.AdmissionError(err)
		}
		tpl = &d.Spec.Template
	case "StatefulSet":
		var s *appsv1.StatefulSet
		if err = json.Unmarshal(req.Object.Raw, &s); err != nil {
			klog.Errorf("Error unmarshaling request with UUID %s in namespace %s, %v", proxyUUID, req.Namespace, err)
			return h.sidecarWebhook.AdmissionError(err)
		}
		tpl = &s.Spec.Template
	}

	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	appcfg, _ := h.sidecarWebhook.GetAppConfig(req.Namespace)
	if appcfg == nil {
		klog.Error("get appcfg is empty")
		return resp
	}

	appName := appcfg.AppName
	if len(appName) == 0 || appName != object.Name {
		return resp
	}

	gpuRequired := appcfg.Requirement.GPU
	if gpuRequired == nil || gpuRequired.IsZero() {
		return resp
	}
	GPUType, err := h.findNvidiaGpuFromNodes(ctx)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(err)
	}

	patchBytes, err := webhook.CreatePatchForDeployment(tpl, req.Namespace, gpuRequired, GPUType)
	if err != nil {
		klog.Errorf("create patch error %v", err)
		return h.sidecarWebhook.AdmissionError(err)
	}
	klog.Info("patchBytes:", string(patchBytes))
	h.sidecarWebhook.PatchAdmissionResponse(resp, patchBytes)
	return resp
}

func (h *Handler) findNvidiaGpuFromNodes(ctx context.Context) (string, error) {
	var nodes corev1.NodeList
	err := h.ctrlClient.List(ctx, &nodes, &client.ListOptions{})
	if err != nil {
		return "", err
	}

	// return nvshare gpu or virtaitech gpu in priority
	gtype := ""
	for _, n := range nodes.Items {
		if _, ok := n.Status.Capacity[constants.NvidiaGPU]; ok {
			if _, ok = n.Status.Capacity[constants.NvshareGPU]; ok {
				return constants.NvshareGPU, nil
			}
			gtype = constants.NvidiaGPU
		}

		if _, ok := n.Status.Capacity[constants.VirtAiTechVGPU]; ok {
			return constants.VirtAiTechVGPU, nil
		}
	}

	if gtype != "" {
		return gtype, nil
	}

	return "", errors.New("gpu node not found")
}

func (h *Handler) providerRegistryValidate(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received provider registry validate webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionReqBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionReqBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decode admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError(err)
	} else {
		admissionResp.Response = h.validateProviderRegistry(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}
	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Errorf("Failed to write response validate review[provider registry] in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Errorf("Done responding to admission[validate provider registry] request with uuid=%s namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) validateProviderRegistry(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission request err=admission request is nil")
		return h.sidecarWebhook.AdmissionError(errNilAdmissionRequest)
	}
	klog.Infof("Enter validate logic namespace=%s name=%s, kind=%s", req.Namespace, req.Name, req.Kind.Kind)
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	// Decode the Object spec from the request.
	obj := &unstructured.Unstructured{}
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &obj)
	if err != nil {
		klog.Errorf("Failed to unmarshal request object raw to unstructured with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(err)
	}
	if obj.Object == nil {
		klog.Errorf("Failed to get object")
		return h.sidecarWebhook.AdmissionError(err)
	}

	dataTypeReq, _, _ := unstructured.NestedString(obj.Object, "spec", "dataType")
	groupReq, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
	versionReq, _, _ := unstructured.NestedString(obj.Object, "spec", "version")

	dClient, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(err)
	}
	prClient := provider.NewRegistryRequest(dClient)
	prs, err := prClient.List(ctx, req.Namespace, metav1.ListOptions{})
	if err != nil {
		return h.sidecarWebhook.AdmissionError(err)
	}
	for _, pr := range prs.Items {
		dataType, _, _ := unstructured.NestedString(pr.Object, "spec", "dataType")
		group, _, _ := unstructured.NestedString(pr.Object, "spec", "group")
		version, _, _ := unstructured.NestedString(pr.Object, "spec", "version")
		if dataType == dataTypeReq && group == groupReq && version == versionReq {
			resp.Allowed = false
			resp.Result = &metav1.Status{Message: "duplicated provider registry with same dataType,group,version"}

		}
	}

	return resp
}
