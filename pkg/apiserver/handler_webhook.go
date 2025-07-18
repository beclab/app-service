package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	iamv1alpha2 "github.com/beclab/api/iam/v1alpha2"
	"strings"

	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/provider"
	"bytetrade.io/web3os/app-service/pkg/upgrade"
	"bytetrade.io/web3os/app-service/pkg/users"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"bytetrade.io/web3os/app-service/pkg/utils/registry"
	"bytetrade.io/web3os/app-service/pkg/webhook"

	wfv1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/containerd/containerd/reference/docker"
	"github.com/emicklei/go-restful/v3"
	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	errNilAdmissionRequest = fmt.Errorf("nil admission request")
)

const (
	deployment              = "Deployment"
	statefulSet             = "StatefulSet"
	applicationNameKey      = "applications.app.bytetrade.io/name"
	applicationGpuInjectKey = "applications.app.bytetrade.io/gpu-inject"
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
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
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
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	var err error
	// Decode the Pod spec from the request
	var pod corev1.Pod
	if err = json.Unmarshal(req.Object.Raw, &pod); err != nil {
		klog.Errorf("Failed to unmarshal admission request object raw to pod with UUID=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	// Start building the response
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	if pod.Spec.HostNetwork && !strings.HasPrefix(req.Namespace, "user-space-") {
		klog.Errorf("Pod with uid=%s namespace=%s has HostNetwork enabled, that's DENIED", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(req.UID, errors.New("HostNetwork Enabled Unsupported"))
	}
	var injectPolicy, injectWs, injectUpload bool
	var perms []appcfg.SysDataPermission
	if injectPolicy, injectWs, injectUpload, perms, err = h.sidecarWebhook.MustInject(ctx, &pod, req.Namespace); err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	klog.Infof("injectPolicy=%v, injectWs=%v, injectUpload=%v, perms=%v", injectPolicy, injectWs, injectUpload, perms)
	if !injectPolicy && !injectWs && !injectUpload && len(perms) == 0 {
		klog.Infof("Skipping sidecar injection for pod with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return resp
	}

	patchBytes, err := h.sidecarWebhook.CreatePatch(ctx, &pod, req, proxyUUID, injectPolicy, injectWs, injectUpload, perms)
	if err != nil {
		klog.Errorf("Failed to create patch for pod uuid=%s name=%s namespace=%s err=%v", proxyUUID, pod.Name, req.Namespace, err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
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
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
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
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
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
		return h.sidecarWebhook.AdmissionError(req.UID, err)
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
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
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
		klog.Errorf("Failed to write response[gpu-limit inject] admin review in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done[gpu-limit inject] with uuid=%s in namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) gpuLimitMutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission Request, err=admission request is nil")
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	klog.Infof("Enter gpuLimitMutate namespace=%s name=%s kind=%s", req.Namespace, req.Name, req.Kind.Kind)

	object := struct {
		metav1.ObjectMeta `json:"metadata,omitempty"`
	}{}
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &object)
	if err != nil {
		klog.Errorf("Error unmarshalling request with UUID %s in namespace %s, error %v ", proxyUUID, req.Namespace, err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	var tpl *corev1.PodTemplateSpec
	annotations := make(map[string]string)

	switch req.Kind.Kind {
	case "Deployment":
		var d *appsv1.Deployment
		if err = json.Unmarshal(req.Object.Raw, &d); err != nil {
			klog.Errorf("Error unmarshaling request with UUID %s in namespace %s, %v", proxyUUID, req.Namespace, err)
			return h.sidecarWebhook.AdmissionError(req.UID, err)
		}
		tpl = &d.Spec.Template
		annotations = d.Annotations
	case "StatefulSet":
		var s *appsv1.StatefulSet
		if err = json.Unmarshal(req.Object.Raw, &s); err != nil {
			klog.Errorf("Error unmarshaling request with UUID %s in namespace %s, %v", proxyUUID, req.Namespace, err)
			return h.sidecarWebhook.AdmissionError(req.UID, err)
		}
		tpl = &s.Spec.Template
		annotations = s.Annotations
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
	if len(appName) == 0 {
		return resp
	}

	gpuRequired := appcfg.Requirement.GPU
	if gpuRequired == nil {
		return resp
	}
	if annotations[applicationGpuInjectKey] != "true" {
		return resp
	}

	GPUType, err := h.findNvidiaGpuFromNodes(ctx)
	if err != nil && !errors.Is(err, api.ErrGPUNodeNotFound) {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	// no gpu found, no need to inject env, just return.
	if GPUType == "" {
		return resp
	}

	terminus, err := upgrade.GetTerminusVersion(ctx, h.ctrlClient)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	nvshareManagedMemory := ""
	if terminus.Spec.Settings != nil {
		nvshareManagedMemory = terminus.Spec.Settings[constants.EnvNvshareManagedMemory]
	}

	envs := []webhook.EnvKeyValue{}
	if nvshareManagedMemory != "" {
		envs = append(envs, webhook.EnvKeyValue{
			Key:   constants.EnvNvshareManagedMemory,
			Value: nvshareManagedMemory,
		})
	}

	envs = append(envs, webhook.EnvKeyValue{Key: "NVSHARE_DEBUG", Value: "1"})

	patchBytes, err := webhook.CreatePatchForDeployment(tpl, req.Namespace, gpuRequired, GPUType, envs)
	if err != nil {
		klog.Errorf("create patch error %v", err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
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

	return "", api.ErrGPUNodeNotFound
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
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
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
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
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
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	if obj.Object == nil {
		klog.Errorf("Failed to get object")
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	dataTypeReq, _, _ := unstructured.NestedString(obj.Object, "spec", "dataType")
	groupReq, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
	versionReq, _, _ := unstructured.NestedString(obj.Object, "spec", "version")
	kindReq, _, _ := unstructured.NestedString(obj.Object, "spec", "kind")

	dClient, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	prClient := provider.NewRegistryRequest(dClient)
	prs, err := prClient.List(ctx, req.Namespace, metav1.ListOptions{})
	if err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	for _, pr := range prs.Items {
		if pr.GetName() == obj.GetName() {
			continue
		}
		if pr.GetDeletionTimestamp() != nil {
			continue
		}
		dataType, _, _ := unstructured.NestedString(pr.Object, "spec", "dataType")
		group, _, _ := unstructured.NestedString(pr.Object, "spec", "group")
		version, _, _ := unstructured.NestedString(pr.Object, "spec", "version")
		kind, _, _ := unstructured.NestedString(pr.Object, "spec", "version")

		if dataType == dataTypeReq && group == groupReq && version == versionReq && kindReq == "provider" && kindReq == kind {
			resp.Allowed = false
			resp.Result = &metav1.Status{Message: fmt.Sprintf("duplicated provider registry with same dataType,group,version, name=%s", pr.GetName())}
			return resp
		}
	}

	return resp
}

func (h *Handler) cronWorkflowInject(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received cron workflow mutating webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionRequestBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		klog.Errorf("Failed to get admission request body")
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionRequestBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decoding admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
	} else {
		admissionResp.Response = h.cronWorkflowMutate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}

	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Infof("cron workflow: write response failed namespace=%s, err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done cron workflow injection admission request with uuid=%s, namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) cronWorkflowMutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission request err=admission request is nil")
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	var wf wfv1alpha1.CronWorkflow
	err := json.Unmarshal(req.Object.Raw, &wf)
	if err != nil {
		klog.Errorf("Failed to unmarshal request object raw with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return resp
	}
	for i, t := range wf.Spec.WorkflowSpec.Templates {
		if t.Container == nil || t.Container.Image == "" {
			continue
		}
		ref, err := docker.ParseDockerRef(t.Container.Image)
		if err != nil {
			continue
		}
		newImage, _ := utils.ReplacedImageRef(registry.GetMirrors(), ref.String(), false)
		wf.Spec.WorkflowSpec.Templates[i].Container.Image = newImage
	}
	original := req.Object.Raw
	current, err := json.Marshal(wf)
	if err != nil {
		klog.Errorf("Failed to marshal cron workflow err=%v", err)
		return resp
	}
	admissionResponse := admission.PatchResponseFromRaw(original, current)
	patchBytes, err := json.Marshal(admissionResponse.Patches)
	if err != nil {
		klog.Errorf("Failed to marshal cron workflow patch bytes err=%v", err)
		return resp
	}
	h.sidecarWebhook.PatchAdmissionResponse(resp, patchBytes)
	return resp
}

func (h *Handler) handleRunAsUser(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received run as user mutate webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionRequestBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		klog.Errorf("Failed to get admission request body")
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionRequestBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decoding admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
	} else {
		admissionResp.Response = h.handleRunAsUserMutate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}

	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Infof("handleRunAsUserMutate: write response failed namespace=%s, err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done handleRunAsUserMutate admission request with uuid=%s, namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) handleRunAsUserMutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission request err=admission request is nil")
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}
	var pod corev1.Pod
	err := json.Unmarshal(req.Object.Raw, &pod)
	if err != nil {
		klog.Errorf("Failed to unmarshal request object raw with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	curPod, err := h.runAsUserInject(ctx, &pod, req.Namespace)
	if err != nil {
		klog.Infof("run runAsUserInject err=%v", err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	current, err := json.Marshal(curPod)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	admissionResp := admission.PatchResponseFromRaw(req.Object.Raw, current)
	patchBytes, err := json.Marshal(admissionResp.Patches)
	if err != nil {
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}
	h.sidecarWebhook.PatchAdmissionResponse(resp, patchBytes)
	return resp
}

func (h *Handler) runAsUserInject(ctx context.Context, pod *corev1.Pod, namespace string) (*corev1.Pod, error) {
	if len(pod.OwnerReferences) == 0 || pod == nil {
		return pod, nil
	}
	var err error
	var kind, name string
	ownerRef := pod.OwnerReferences[0]
	switch ownerRef.Kind {
	case "ReplicaSet":
		key := types.NamespacedName{Namespace: namespace, Name: ownerRef.Name}
		var rs appsv1.ReplicaSet
		err = h.ctrlClient.Get(ctx, key, &rs)
		if err != nil {
			klog.Infof("get replicaset err=%v", err)
			return nil, err
		}
		if len(rs.OwnerReferences) > 0 && rs.OwnerReferences[0].Kind == deployment {
			kind = deployment
			name = rs.OwnerReferences[0].Name
		}
	case statefulSet:
		kind = statefulSet
		name = ownerRef.Name
	}
	if kind == "" {
		return pod, nil
	}
	labels := make(map[string]string)
	switch kind {
	case deployment:
		var deploy appsv1.Deployment
		key := types.NamespacedName{Name: name, Namespace: namespace}
		err = h.ctrlClient.Get(ctx, key, &deploy)
		if err != nil {
			return nil, err
		}
		labels = deploy.Labels

	case statefulSet:
		var sts appsv1.StatefulSet
		key := types.NamespacedName{Name: name, Namespace: namespace}
		err = h.ctrlClient.Get(ctx, key, &sts)
		if err != nil {
			return nil, err
		}
		labels = sts.Labels
	}
	userID := int64(1000)
	if appName, ok := labels[applicationNameKey]; ok && !userspace.IsSysApp(appName) &&
		labels[constants.ApplicationRunAsUserLabel] == "true" {
		if pod.Spec.SecurityContext == nil {
			pod.Spec.SecurityContext = &corev1.PodSecurityContext{
				RunAsUser: &userID,
			}
		} else {
			if pod.Spec.SecurityContext.RunAsUser == nil || *pod.Spec.SecurityContext.RunAsUser != 1000 {
				pod.Spec.SecurityContext.RunAsUser = &userID
			}
		}
		return pod, nil
	}

	return pod, nil
}

func (h *Handler) appLabelInject(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received mutating webhook[app-label inject] request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionRequestBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionRequestBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decode admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
	} else {
		admissionResp.Response = h.appLabelMutate(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}
	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Errorf("Failed to write response[app-label inject] admin review in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done[app-label inject] with uuid=%s in namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) appLabelMutate(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission Request, err=admission request is nil")
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	klog.Infof("Enter appLabelMutate namespace=%s name=%s kind=%s", req.Namespace, req.Name, req.Kind.Kind)

	object := struct {
		metav1.ObjectMeta `json:"metadata,omitempty"`
	}{}
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &object)
	if err != nil {
		klog.Errorf("Error unmarshalling request with UUID %s in namespace %s, error %v ", proxyUUID, req.Namespace, err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	appCfg, _ := h.sidecarWebhook.GetAppConfig(req.Namespace)
	if appCfg == nil {
		klog.Error("get appcfg is empty")
		return resp
	}

	appName := appCfg.AppName
	if len(appName) == 0 {
		return resp
	}

	patchBytes, err := makePatches(req, appCfg)
	if err != nil {
		klog.Errorf("make patches err=%v", patchBytes)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	klog.Info("patchBytes:", string(patchBytes))
	h.sidecarWebhook.PatchAdmissionResponse(resp, patchBytes)
	return resp
}

func makePatches(req *admissionv1.AdmissionRequest, appCfg *appcfg.ApplicationConfig) ([]byte, error) {
	original := req.Object.Raw
	var patchBytes []byte
	var tpl *corev1.PodTemplateSpec
	switch req.Kind.Kind {
	case "Deployment":
		var deploy *appsv1.Deployment
		if err := json.Unmarshal(req.Object.Raw, &deploy); err != nil {
			klog.Errorf("Error unmarshal request in namespace %s, %v", req.Namespace, err)
			return []byte{}, err
		}
		tpl = &deploy.Spec.Template
		if tpl.ObjectMeta.Labels == nil {
			tpl.ObjectMeta.Labels = make(map[string]string)
		}
		tpl.ObjectMeta.Labels["io.bytetrade.app"] = "true"
		tpl.ObjectMeta.Labels[constants.ApplicationNameLabel] = appCfg.AppName
		tpl.ObjectMeta.Labels[constants.ApplicationOwnerLabel] = appCfg.OwnerName
		current, err := json.Marshal(deploy)
		if err != nil {
			return []byte{}, err
		}
		admissionResponse := admission.PatchResponseFromRaw(original, current)
		patchBytes, err = json.Marshal(admissionResponse.Patches)
		if err != nil {
			return []byte{}, err
		}
	case "StatefulSet":
		var sts *appsv1.StatefulSet
		if err := json.Unmarshal(req.Object.Raw, &sts); err != nil {
			klog.Errorf("Error unmarshaling request in namespace %s, %v", req.Namespace, err)
			return []byte{}, err
		}
		tpl = &sts.Spec.Template
		if tpl.ObjectMeta.Labels == nil {
			tpl.ObjectMeta.Labels = make(map[string]string)
		}
		tpl.ObjectMeta.Labels["io.bytetrade.app"] = "true"
		tpl.ObjectMeta.Labels[constants.ApplicationNameLabel] = appCfg.AppName
		tpl.ObjectMeta.Labels[constants.ApplicationOwnerLabel] = appCfg.OwnerName
		current, err := json.Marshal(sts)
		if err != nil {
			return []byte{}, err
		}
		admissionResponse := admission.PatchResponseFromRaw(original, current)
		patchBytes, err = json.Marshal(admissionResponse.Patches)
		if err != nil {
			return []byte{}, err
		}
	}
	return patchBytes, nil
}

func (h *Handler) userValidate(req *restful.Request, resp *restful.Response) {
	klog.Infof("Received user validate webhook request: Method=%v, URL=%v", req.Request.Method, req.Request.URL)
	admissionReqBody, ok := h.sidecarWebhook.GetAdmissionRequestBody(req, resp)
	if !ok {
		return
	}
	var admissionReq, admissionResp admissionv1.AdmissionReview
	proxyUUID := uuid.New()
	if _, _, err := webhook.Deserializer.Decode(admissionReqBody, nil, &admissionReq); err != nil {
		klog.Errorf("Failed to decode admission request body err=%v", err)
		admissionResp.Response = h.sidecarWebhook.AdmissionError("", err)
	} else {
		admissionResp.Response = h.validateUser(req.Request.Context(), admissionReq.Request, proxyUUID)
	}
	admissionResp.TypeMeta = admissionReq.TypeMeta
	admissionResp.Kind = admissionReq.Kind

	requestForNamespace := "unknown"
	if admissionReq.Request != nil {
		requestForNamespace = admissionReq.Request.Namespace
	}
	err := resp.WriteAsJson(&admissionResp)
	if err != nil {
		klog.Errorf("Failed to write response validate review[user] in namespace=%s err=%v", requestForNamespace, err)
		return
	}
	klog.Infof("Done responding to admission[validate user] request with uuid=%s namespace=%s", proxyUUID, requestForNamespace)
}

func (h *Handler) validateUser(ctx context.Context, req *admissionv1.AdmissionRequest, proxyUUID uuid.UUID) *admissionv1.AdmissionResponse {
	if req == nil {
		klog.Error("Failed to get admission request err=admission request is nil")
		return h.sidecarWebhook.AdmissionError("", errNilAdmissionRequest)
	}
	klog.Infof("Enter validate user logic namespace=%s name=%s, kind=%s", req.Namespace, req.Name, req.Kind.Kind)
	resp := &admissionv1.AdmissionResponse{
		Allowed: true,
		UID:     req.UID,
	}

	// Decode the User spec from the request.
	var user iamv1alpha2.User
	raw := req.Object.Raw
	err := json.Unmarshal(raw, &user)
	if err != nil {
		klog.Errorf("Failed to unmarshal request object raw to user with uuid=%s namespace=%s", proxyUUID, req.Namespace)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	// Check if user already exists
	existingUsers := &iamv1alpha2.UserList{}
	err = h.ctrlClient.List(ctx, existingUsers)
	if err != nil {
		klog.Errorf("Failed to list existing users: %v", err)
		return h.sidecarWebhook.AdmissionError(req.UID, err)
	}

	for _, existingUser := range existingUsers.Items {
		if existingUser.Name == user.Name {
			resp.Allowed = false
			resp.Result = &metav1.Status{
				Message: fmt.Sprintf("User with name '%s' already exists", user.Name),
			}
			return resp
		}
	}
	if v, _ := user.Annotations[users.UserAnnotationIsEphemeral]; v == "false" || v == "" {
		return resp
	}

	if len(user.Spec.InitialPassword) < 8 {
		resp.Allowed = false
		resp.Result = &metav1.Status{
			Message: fmt.Sprintf("invalid initial password lenth must greater than 8 char"),
		}
		return resp
	}

	ownerRole := user.Annotations[users.UserAnnotationOwnerRole]

	creator := user.Annotations[users.AnnotationUserCreator]
	isValidCreator := false
	for _, existingUser := range existingUsers.Items {
		//existingUserRole := existingUser.Annotations[users.UserAnnotationOwnerRole]

		if existingUser.Name == creator {
			isValidCreator = true
		}
	}
	if !isValidCreator {
		resp.Allowed = false
		resp.Result = &metav1.Status{
			Message: fmt.Sprintf("invalid creator %s", creator),
		}
		return resp
	}

	if ownerRole != "owner" && ownerRole != "admin" && ownerRole != "normal" {
		resp.Allowed = false
		resp.Result = &metav1.Status{
			Message: fmt.Sprintf("invalid owner role: %s", ownerRole),
		}
		return resp
	}
	err = users.ValidateResourceLimits(&user)
	if err != nil {
		resp.Allowed = false
		resp.Result = &metav1.Status{
			Message: fmt.Sprintf("invalid cpu or memory limit: %s", err),
		}
		return resp
	}

	klog.Infof("User validation passed for user=%s with UID=%s", user.Name, user.UID)
	return resp
}

func isInPrivateNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, "user-space-") || strings.HasPrefix(namespace, "user-system-")
}

//func checkCreatorRole(creatorRole string, ownerRole string) bool {
//	m := map[string]string {
//		"owner"
//	}
//}
