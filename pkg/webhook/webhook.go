package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	appcfg_mod "bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/provider"
	"bytetrade.io/web3os/app-service/pkg/sandbox/sidecar"
	"bytetrade.io/web3os/app-service/pkg/security"
	"bytetrade.io/web3os/app-service/pkg/utils"

	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"github.com/emicklei/go-restful/v3"
	"github.com/google/uuid"
	"github.com/thoas/go-funk"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	errEmptyAdmissionRequestBody = fmt.Errorf("empty request admission request body")

	// codecs is the codec factory used by the deserializer.
	codecs = serializer.NewCodecFactory(runtime.NewScheme())

	// Deserializer is used to decode the admission request body.
	Deserializer = codecs.UniversalDeserializer()

	// UUIDAnnotation uuid key for annotation.
	UUIDAnnotation = "sidecar.bytetrade.io/proxy-uuid"
)

// Webhook used to implement a webhook.
type Webhook struct {
	kubeClient    *kubernetes.Clientset
	dynamicClient *versioned.Clientset
}

// New create a webhook client.
func New(config *rest.Config) (*Webhook, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := versioned.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Webhook{
		kubeClient:    client,
		dynamicClient: dynamicClient,
	}, nil
}

// GetAppConfig get app config by namespace.
func (wh *Webhook) GetAppConfig(namespace string) (*appcfg_mod.ApplicationConfig, error) {
	list, err := wh.dynamicClient.AppV1alpha1().ApplicationManagers().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	sorted := list.Items
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[j].CreationTimestamp.Before(&sorted[i].CreationTimestamp)
	})

	var appconfig appcfg.ApplicationConfig
	for _, a := range sorted {
		if a.Spec.AppNamespace == namespace && a.Spec.Type == v1alpha1.App {
			err = json.Unmarshal([]byte(a.Spec.Config), &appconfig)
			if err != nil {
				return nil, err
			}
			return &appconfig, nil
		}
	}
	return nil, errors.New("not found appmgr")
}

// GetAdmissionRequestBody returns admission request body.
func (wh *Webhook) GetAdmissionRequestBody(req *restful.Request, resp *restful.Response) ([]byte, bool) {
	emptyBodyError := func() ([]byte, bool) {
		klog.Error("Failed to read admission request body err=body is empty")
		api.HandleBadRequest(resp, req, errEmptyAdmissionRequestBody)
		return nil, false
	}

	if req.Request.Body == nil {
		return emptyBodyError()
	}

	admissionRequestBody, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		api.HandleInternalError(resp, req, err)
		klog.Errorf("Failed to  read admission request body; Responded to admission request with HTTP=%v err=%v", http.StatusInternalServerError, err)
		return admissionRequestBody, false
	}

	if len(admissionRequestBody) == 0 {
		return emptyBodyError()
	}

	return admissionRequestBody, true
}

// CreatePatch create a patch for a pod.
func (wh *Webhook) CreatePatch(
	ctx context.Context,
	pod *corev1.Pod,
	req *admissionv1.AdmissionRequest,
	proxyUUID uuid.UUID, injectPolicy, injectWs, injectUpload bool, perms []appcfg.SysDataPermission) ([]byte, error) {
	isInjected, prevUUID := isInjectedPod(pod)

	if isInjected {
		// TODO: force mutate
		klog.Infof("Pod is injected with uuid=%s namespace=%s", prevUUID, req.Namespace)
		return makePatches(req, pod)
	}

	configMapName, err := wh.createSidecarConfigMap(ctx, pod, proxyUUID.String(), req.Namespace, injectPolicy, injectWs, injectUpload, perms)
	if err != nil {
		return nil, err
	}
	appcfg, err := wh.GetAppConfig(req.Namespace)
	if err != nil {
		return nil, err
	}

	volume := sidecar.GetSidecarVolumeSpec(configMapName)

	if pod.Spec.Volumes == nil {
		pod.Spec.Volumes = []corev1.Volume{}
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, volume, sidecar.GetEnvoyConfigWorkVolume())

	initContainer := sidecar.GetInitContainerSpec(appcfg)
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

	clusterID := fmt.Sprintf("%s.%s", pod.Spec.ServiceAccountName, req.Name)
	envoyFilename := constants.EnvoyConfigFilePath + "/" + constants.EnvoyConfigFileName
	// pod is not an entrance pod, just inject outbound proxy
	if !injectPolicy {
		envoyFilename = constants.EnvoyConfigFilePath + "/" + constants.EnvoyConfigOnlyOutBoundFileName
	}
	appKey, appSecret, _ := wh.getAppKeySecret(req.Namespace)

	policySidecar := sidecar.GetEnvoySidecarContainerSpec(clusterID, envoyFilename, appKey, appSecret)
	pod.Spec.Containers = append(pod.Spec.Containers, policySidecar)

	pod.Spec.InitContainers = append(
		[]corev1.Container{
			sidecar.GetInitContainerSpecForWaitFor(appcfg.OwnerName),
			sidecar.GetInitContainerSpecForRenderEnvoyConfig(),
		},
		pod.Spec.InitContainers...)

	if injectWs {

		wsSidecar := sidecar.GetWebSocketSideCarContainerSpec(&appcfg.WsConfig)
		pod.Spec.Containers = append(pod.Spec.Containers, wsSidecar)
	}
	if injectUpload {
		uploadSidecar := sidecar.GetUploadSideCarContainerSpec(pod, &appcfg.Upload)
		if uploadSidecar != nil {
			pod.Spec.Containers = append(pod.Spec.Containers, *uploadSidecar)
		}
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[UUIDAnnotation] = proxyUUID.String()
	return makePatches(req, pod)
}

// PatchAdmissionResponse returns an admission response with patch data.
func (wh *Webhook) PatchAdmissionResponse(resp *admissionv1.AdmissionResponse, patchBytes []byte) {
	resp.Patch = patchBytes
	pt := admissionv1.PatchTypeJSONPatch
	resp.PatchType = &pt
}

// AdmissionError wraps error as AdmissionResponse
func (wh *Webhook) AdmissionError(uid types.UID, err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		UID: uid,
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// MustInject checks which inject operation should do for a pod.
func (wh *Webhook) MustInject(ctx context.Context, pod *corev1.Pod, namespace string) (bool, bool, bool, []appcfg.SysDataPermission, error) {
	perms := make([]appcfg.SysDataPermission, 0)
	if !isNamespaceInjectable(namespace) {
		return false, false, false, perms, nil
	}

	// TODO: uninject annotation

	// get appLabel from namespace
	_, err := wh.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get namespace=%s err=%v", namespace, err)
		return false, false, false, perms, err
	}

	appcfg, _ := wh.GetAppConfig(namespace)
	if appcfg == nil {
		klog.Infof("Unknown namespace=%s, do not inject", namespace)
		return false, false, false, perms, nil
	}

	var injectWs, injectUpload bool
	if appcfg.WsConfig.URL != "" && appcfg.WsConfig.Port > 0 {
		injectWs = true
	}
	if appcfg.Upload.Dest != "" {
		injectUpload = true
	}
	for _, p := range appcfg.Permission {
		if sysDataP, ok := p.([]interface{}); ok {
			for _, v := range sysDataP {
				sysData := v.(map[string]interface{})
				var ns string
				if val, ok := sysData["namespace"].(string); ok {
					ns = val
				}
				providerAppName := sysData["appName"].(string)
				providerName := sysData["providerName"].(string)
				perms = append(perms, appcfg_mod.SysDataPermission{
					AppName:      providerAppName,
					Namespace:    ns,
					ProviderName: providerName,
				})

			}
		}

	}
	for _, e := range appcfg.Entrances {
		isEntrancePod, err := wh.isAppEntrancePod(ctx, appcfg.AppName, e.Host, pod, namespace)
		klog.Infof("entranceName=%s isEntrancePod=%v", e.Name, isEntrancePod)
		if err != nil {
			return false, false, false, perms, err
		}

		if isEntrancePod {
			return true, injectWs, injectUpload, perms, nil
		}
	}

	return false, injectWs, injectUpload, perms, nil
}

func (wh *Webhook) isAppEntrancePod(ctx context.Context, appname, host string, pod *corev1.Pod, namespace string) (bool, error) {
	service, err := wh.kubeClient.CoreV1().Services(namespace).Get(ctx, host, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get app service appName=%s host=%s err=%v", appname, host, err)
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	selector, err := labels.ValidatedSelectorFromSet(service.Spec.Selector)
	if err != nil {
		klog.Errorf("Failed to get service selector appName=%s host=%s err=%v", appname, host, err)
		return false, err
	}

	return selector.Matches(labels.Set(pod.GetLabels())), nil
}

func (wh *Webhook) createSidecarConfigMap(
	ctx context.Context, pod *corev1.Pod,
	proxyUUID, namespace string, injectPolicy, injectWs, injectUpload bool,
	perms []appcfg_mod.SysDataPermission,
) (string, error) {
	configMapName := fmt.Sprintf("%s-%s", constants.SidecarConfigMapVolumeName, proxyUUID)
	if deployName := utils.GetDeploymentName(pod); deployName != "" {
		configMapName = fmt.Sprintf("%s-%s", configMapName, deployName)
	}
	cm, e := wh.kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if e != nil && !apierrors.IsNotFound(e) {
		return "", e
	}

	appcfg, err := wh.GetAppConfig(namespace)
	if err != nil {
		klog.Errorf("Failed to get app config err=%v", err)
		return "", err
	}

	permCfg, err := apputils.ProviderPermissionsConvertor(perms).ToPermissionCfg(ctx, appcfg.OwnerName)
	if err != nil {
		klog.Errorf("Failed to convert permissions for app %s: %v", appcfg.AppName, err)
		return "", err
	}

	newConfigMap := sidecar.GetSidecarConfigMap(configMapName, namespace, appcfg, injectPolicy, injectWs, injectUpload, pod, permCfg)
	if e == nil {
		// configmap found
		cm.Data = newConfigMap.Data
		if _, err := wh.kubeClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("Failed to update sidecar configmap=%s in namespace=%s err=%v", configMapName, namespace, err)
			return "", err
		}
	} else {
		if _, err := wh.kubeClient.CoreV1().ConfigMaps(namespace).Create(ctx, newConfigMap, metav1.CreateOptions{}); err != nil {
			klog.Errorf("Failed to create sidecar configmap=%s in namespace=%s err=%v", configMapName, namespace, err)
			return "", err
		}
	}

	return configMapName, nil
}

func isNamespaceInjectable(namespace string) bool {
	if security.IsUnderLayerNamespace(namespace) {
		return false
	}

	if security.IsOSSystemNamespace(namespace) {
		return false
	}

	if ok, _ := security.IsUserInternalNamespaces(namespace); ok {
		return false
	}

	return true
}

func isInjectedPod(pod *corev1.Pod) (bool, string) {
	if pod.Annotations != nil {
		if proxyUUID, ok := pod.Annotations[UUIDAnnotation]; ok {
			for _, c := range pod.Spec.Containers {
				if c.Name == constants.EnvoyContainerName {
					return true, proxyUUID
				}
			}
		}
	}

	for _, c := range pod.Spec.InitContainers {
		if c.Name == constants.SidecarInitContainerName {
			return true, ""
		}
	}

	return false, ""
}

func makePatches(req *admissionv1.AdmissionRequest, pod *corev1.Pod) ([]byte, error) {
	original := req.Object.Raw
	current, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf("Failed to  marshal pod with UID=%s", pod.ObjectMeta.UID)
	}
	admissionResponse := admission.PatchResponseFromRaw(original, current)
	return json.Marshal(admissionResponse.Patches)
}

type patchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var resourcePath = "/spec/template/spec/containers/%d/resources/limits"
var envPath = "/spec/template/spec/containers/%d/env/%s"
var runtimeClassPath = "/spec/template/spec/runtimeClassName"

type EnvKeyValue struct {
	Key   string
	Value string
}

// CreatePatchForDeployment add gpu env for deployment and returns patch bytes.
func CreatePatchForDeployment(tpl *corev1.PodTemplateSpec, namespace string, gpuRequired *resource.Quantity, typeKey string, envKeyValues []EnvKeyValue) ([]byte, error) {
	patches, err := addResourceLimits(tpl, namespace, gpuRequired, typeKey, envKeyValues)
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(patches)
}

func addResourceLimits(tpl *corev1.PodTemplateSpec, namespace string, gpuRequired *resource.Quantity, typeKey string, envKeyValues []EnvKeyValue) (patch []patchOp, err error) {
	if typeKey == constants.NvidiaGPU || typeKey == constants.NvshareGPU {
		if tpl.Spec.RuntimeClassName != nil {
			patch = append(patch, patchOp{
				Op:    constants.PatchOpReplace,
				Path:  runtimeClassPath,
				Value: "nvidia",
			})
		} else {
			patch = append(patch, patchOp{
				Op:    constants.PatchOpAdd,
				Path:  runtimeClassPath,
				Value: "nvidia",
			})
		}
	}

	for i := range tpl.Spec.Containers {
		container := tpl.Spec.Containers[i]

		if len(container.Resources.Limits) == 0 {
			patch = append(patch, patchOp{
				Op:   constants.PatchOpAdd,
				Path: fmt.Sprintf(resourcePath, i),
				Value: map[string]interface{}{
					typeKey: "1",
				},
			})
		} else {
			t := make(map[string]map[string]string)
			t["limits"] = map[string]string{}
			for k, v := range container.Resources.Limits {
				if k.String() == constants.NvidiaGPU || k.String() == constants.NvshareGPU || k.String() == constants.VirtAiTechVGPU {
					continue
				}
				t["limits"][k.String()] = v.String()
			}
			t["limits"][typeKey] = "1"
			patch = append(patch, patchOp{
				Op:    constants.PatchOpReplace,
				Path:  fmt.Sprintf(resourcePath, i),
				Value: t["limits"],
			})
		}
		envNames := make([]string, 0)
		if len(container.Env) == 0 {
			value := make([]map[string]string, 0)
			for _, e := range envKeyValues {
				if e.Value == "" {
					continue
				}
				envNames = append(envNames, e.Key)
				value = append(value, map[string]string{
					"name":  e.Key,
					"value": e.Value,
				})
			}
			op := patchOp{
				Op:    "add",
				Path:  fmt.Sprintf("/spec/template/spec/containers/%d/env", i),
				Value: value,
			}
			patch = append(patch, op)
		} else {
			for envIdx, env := range container.Env {
				for _, e := range envKeyValues {
					if e.Value == "" {
						continue
					}
					if env.Name == e.Key {
						envNames = append(envNames, env.Name)
						patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, e.Key, e.Value)...)
					}
				}
			}
		}
		for _, env := range envKeyValues {
			if !funk.Contains(envNames, env.Key) {
				patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, env.Key, env.Value)...)
			}
		}

	}

	return patch, nil
}

func genPatchesForEnv(op string, containerIdx, envIdx int, name, value string) (patch []patchOp) {
	envIndexString := "-"
	if op == constants.PatchOpReplace {
		envIndexString = strconv.Itoa(envIdx)
	}
	patch = append(patch, patchOp{
		Op:   op,
		Path: fmt.Sprintf(envPath, containerIdx, envIndexString),
		Value: map[string]string{
			"name":  name,
			"value": value,
		},
	})
	return patch
}

func (wh *Webhook) getAppKeySecret(namespace string) (string, string, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return "", "", err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", "", err
	}
	appcfg, err := wh.GetAppConfig(namespace)
	if err != nil {
		klog.Errorf("Failed to get app config err=%v", err)
		return "", "", err
	}

	apClient := provider.NewApplicationPermissionRequest(client)
	ap, err := apClient.Get(context.TODO(), "user-system-"+appcfg.OwnerName, appcfg.AppName, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	var appKey, appSecret string
	if ap != nil {
		appKey, _, _ = unstructured.NestedString(ap.Object, "spec", "key")
		appSecret, _, _ = unstructured.NestedString(ap.Object, "spec", "secret")
		return appKey, appSecret, nil
	}
	return "", "", errors.New("nil applicationpermission object")
}
