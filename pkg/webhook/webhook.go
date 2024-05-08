package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"

	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/sandbox/sidecar"
	"bytetrade.io/web3os/app-service/pkg/security"

	"github.com/emicklei/go-restful/v3"
	"github.com/google/uuid"
	"github.com/thoas/go-funk"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
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
func (wh *Webhook) GetAppConfig(namespace string) (*appinstaller.ApplicationConfig, error) {
	list, err := wh.dynamicClient.AppV1alpha1().ApplicationManagers().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var appconfig appinstaller.ApplicationConfig
	for _, a := range list.Items {
		if a.Spec.AppNamespace == namespace {
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
	proxyUUID uuid.UUID, injectPolicy, injectWs, injectUpload bool) ([]byte, error) {
	isInjected, prevUUID := isInjectedPod(pod)

	if isInjected {
		// TODO: force mutate
		klog.Infof("Pod is injected with uuid=%s namespace=%s", prevUUID, req.Namespace)
		return makePatches(req, pod)
	}

	configMapName, err := wh.createSidecarConfigMap(ctx, pod, proxyUUID.String(), req.Namespace, injectPolicy, injectWs, injectUpload)
	if err != nil {
		return nil, err
	}

	volume := sidecar.GetSidecarVolumeSpec(configMapName)

	if pod.Spec.Volumes == nil {
		pod.Spec.Volumes = []corev1.Volume{}
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, volume)

	initContainer := sidecar.GetInitContainerSpec()
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

	policySidecar := sidecar.GetEnvoySidecarContainerSpec(pod, req.Namespace)
	pod.Spec.Containers = append(pod.Spec.Containers, policySidecar)

	if injectWs {
		appcfg, err := wh.GetAppConfig(req.Namespace)
		if err != nil {
			return nil, err
		}
		wsSidecar := sidecar.GetWebSocketSideCarContainerSpec(&appcfg.WsConfig)
		pod.Spec.Containers = append(pod.Spec.Containers, wsSidecar)
	}
	if injectUpload {
		appcfg, err := wh.GetAppConfig(req.Namespace)
		if err != nil {
			return nil, err
		}
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
func (wh *Webhook) AdmissionError(err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// MustInject checks which inject operation should do for a pod.
func (wh *Webhook) MustInject(ctx context.Context, pod *corev1.Pod, namespace string) (bool, bool, bool, error) {
	if !isNamespaceInjectable(namespace) {
		return false, false, false, nil
	}

	// TODO: uninject annotation

	// get appLabel from namespace
	_, err := wh.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		klog.Error("Failed to get namespace=%s err=%v", namespace, err)
		return false, false, false, err
	}

	appcfg, _ := wh.GetAppConfig(namespace)
	if appcfg == nil {
		klog.Infof("Unknown namespace=%s, do not inject", namespace)
		return false, false, false, nil
	}
	var injectWs, injectUpload bool
	if appcfg.WsConfig.URL != "" && appcfg.WsConfig.Port > 0 {
		injectWs = true
	}
	if appcfg.Upload.Dest != "" {
		injectUpload = true
	}

	for _, e := range appcfg.Entrances {
		isEntrancePod, err := wh.isAppEntrancePod(ctx, appcfg.AppName, e.Host, pod, namespace)
		klog.Infof("entranceName=%s isEntrancePod=%v", e.Name, isEntrancePod)
		if err != nil {
			return false, false, false, err
		}
		if isEntrancePod {
			return true, injectWs, injectUpload, nil
		}
	}

	return false, injectWs, injectUpload, nil
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
) (string, error) {
	configMapName := fmt.Sprintf("%s-%s", constants.SidecarConfigMapVolumeName, proxyUUID)
	cm, e := wh.kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if e != nil && !apierrors.IsNotFound(e) {
		return "", e
	}

	appcfg, err := wh.GetAppConfig(namespace)
	if err != nil {
		klog.Errorf("Failed to get app config err=%v", err)
		return "", err
	}
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		return "", err
	}
	zone, err := kubesphere.GetUserZone(ctx, kubeConfig, appcfg.OwnerName)
	if err != nil {
		return "", err
	}

	appDomains := make([]string, 0)
	if appcfg.ResetCookieEnabled {
		if len(appcfg.Entrances) == 1 {
			appDomains = append(appDomains, fmt.Sprintf("%s.%s", appcfg.AppID, zone))
			appDomains = append(appDomains, fmt.Sprintf("%s.local.%s", appcfg.AppID, zone))
		} else {
			for i := range appcfg.Entrances {
				appDomains = append(appDomains, fmt.Sprintf("%s%d.%s", appcfg.AppID, i, zone))
				appDomains = append(appDomains, fmt.Sprintf("%s%d.local.%s", appcfg.AppID, i, zone))
			}
		}
	}

	newConfigMap := sidecar.GetSidecarConfigMap(configMapName, namespace, appcfg.OwnerName, injectPolicy, injectWs, injectUpload, appDomains, pod)
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

var resourcePath = "/spec/template/spec/containers/%d/resources"
var envPath = "/spec/template/spec/containers/%d/env/%s"

// CreatePatchForDeployment add gpu env for deployment and returns patch bytes.
func CreatePatchForDeployment(tpl *corev1.PodTemplateSpec, namespace string, gpuRequired *resource.Quantity, typeKey string) ([]byte, error) {
	patches, err := addResourceLimits(tpl, namespace, gpuRequired, typeKey)
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(patches)
}

func addResourceLimits(tpl *corev1.PodTemplateSpec, namespace string, gpuRequired *resource.Quantity, typeKey string) (patch []patchOp, err error) {
	for i := range tpl.Spec.Containers {
		container := tpl.Spec.Containers[i]

		if len(container.Resources.Limits) == 0 {
			patch = append(patch, patchOp{
				Op:   constants.PatchOpAdd,
				Path: fmt.Sprintf(resourcePath, i),
				Value: map[string]interface{}{
					"limits": map[string]interface{}{
						typeKey: "1",
					},
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
				Value: t,
			})
		}
		if typeKey == constants.VirtAiTechVGPU {
			gmem := int(math.Ceil(gpuRequired.AsApproximateFloat64() / 1024 / 1024))
			envNames := make([]string, 0)
			for envIdx, env := range container.Env {
				if env.Name == constants.EnvOrionVGPU {
					envNames = append(envNames, env.Name)
					patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, constants.EnvOrionVGPU, "1")...)
				}
				if env.Name == constants.EnvOrionClientID {
					envNames = append(envNames, env.Name)
					patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, constants.EnvOrionClientID, namespace)...)
				}
				if env.Name == constants.EnvOrionTaskName {
					envNames = append(envNames, env.Name)
					patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, constants.EnvOrionTaskName,
						fmt.Sprintf("%s-%s", namespace, container.Name))...)
				}
				if env.Name == constants.EnvOrionGMEM {
					envNames = append(envNames, env.Name)
					patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, constants.EnvOrionGMEM, strconv.Itoa(gmem))...)
				}
				if env.Name == constants.EnvOrionReserved {
					envNames = append(envNames, env.Name)
					patch = append(patch, genPatchesForEnv(constants.PatchOpReplace, i, envIdx, constants.EnvOrionReserved, "0")...)
				}
			}

			envs := []string{constants.EnvOrionVGPU, constants.EnvOrionClientID, constants.EnvOrionTaskName,
				constants.EnvOrionGMEM, constants.EnvOrionReserved}

			for _, name := range envs {
				if !funk.Contains(envNames, name) {
					if name == constants.EnvOrionVGPU {
						patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, name, "1")...)
					}
					if name == constants.EnvOrionClientID {
						patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, name, namespace)...)
					}
					if name == constants.EnvOrionTaskName {
						patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, name,
							fmt.Sprintf("%s-%s", namespace, container.Name))...)
					}
					if name == constants.EnvOrionGMEM {
						patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, name, strconv.Itoa(gmem))...)
					}
					if name == constants.EnvOrionReserved {
						patch = append(patch, genPatchesForEnv(constants.PatchOpAdd, i, -1, name, "0")...)
					}
				}
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
