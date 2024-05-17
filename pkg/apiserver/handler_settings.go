package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"strings"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/helm"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"

	"github.com/emicklei/go-restful/v3"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func (h *Handler) setupApp(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}

	bodyData, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	var settings map[string]interface{}
	err = json.Unmarshal(bodyData, &settings)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	appCopy := app.DeepCopy()

	// TODO: validate settings keys
	for k, v := range settings {
		var str []byte
		switch v.(type) {
		case map[string]interface{}:
			str, err = json.Marshal(v)
			if err != nil {
				api.HandleError(resp, req, err)
				return
			}
		default:
			str = []byte(v.(string))
		}
		appCopy.Spec.Settings[k] = string(str)
	}
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)

	appUpdated, err := client.AppClient.AppV1alpha1().Applications().Update(req.Request.Context(), appCopy, metav1.UpdateOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(appUpdated.Spec.Settings)
}

func (h *Handler) setupAppEntranceDomain(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		api.HandleError(resp, req, err)
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}

	entranceName := req.PathParameter(ParamEntranceName)
	validName := false
	for _, e := range app.Spec.Entrances {
		if e.Name == entranceName {
			validName = true
		}
	}
	if !validName {
		api.HandleBadRequest(resp, req, errors.New("invalid entrance name"))
	}

	bodyData, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	var settings map[string]interface{}
	err = json.Unmarshal(bodyData, &settings)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	appCopy := app.DeepCopy()

	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)

	customDomain, ok := settings["customDomain"].(map[string]interface{})

	// get the origin custom domain settings and do a merge
	a := appCopy.Spec.Settings["customDomain"]
	merge := make(map[string]interface{})

	keys := []string{"third_level_domain", "third_party_domain"}

	if len(a) > 0 {
		var origins map[string]interface{}
		err = json.Unmarshal([]byte(a), &origins)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		// do a merge
		// origins {"a":{"third_level_domain":"","third_party__domain":""},"b":{"third_level_domain":"","third_party__domain":""}}
		// {"third_level_domain":"","third_party__domain":""}
		for k, v := range origins {
			originV := v.(map[string]interface{})
			if k != entranceName {
				merge[k] = originV
				continue
			} else {
				for _, key := range keys {
					if ov, ok := originV[key]; ok {
						if _, exists := customDomain[key]; !exists {
							customDomain[key] = ov
						}
					}
				}
			}
		}
	}
	for _, key := range keys {
		if _, exists := customDomain[key]; !exists {
			customDomain[key] = ""
		}
	}
	merge[entranceName] = customDomain

	settingsBytes, err := json.Marshal(merge)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"settings": map[string]string{
				"customDomain": string(settingsBytes),
			},
		},
	}
	patchByte, err := json.Marshal(patchData)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	appUpdated, err := client.AppClient.AppV1alpha1().Applications().Patch(req.Request.Context(), appCopy.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if ok {
		// upgrade app set values
		owner := req.Attribute(constants.UserContextAttribute).(string)
		repoURL, err := getRepoURL(client, owner)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		actionConfig, cliSttings, err := helm.InitConfig(h.kubeConfig, app.Spec.Namespace)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		zone, err := kubesphere.GetUserZone(req.Request.Context(), h.kubeConfig, owner)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}

		vals := make(map[string]interface{})
		entries := make(map[string]interface{})
		for i, entrance := range app.Spec.Entrances {
			cfg, ok := customDomain[entrance.Name].(map[string]interface{})
			if !ok {
				continue
			}
			urls := make([]string, 0)
			if cDomain, _ := cfg["third_party_domain"].(string); cDomain != "" {
				urls = append(urls, cDomain)
			}
			if prefix, _ := cfg["third_level_domain"]; prefix != "" {
				urls = append(urls, fmt.Sprintf("%s.%s", prefix, zone))
			}
			var url string
			if len(app.Spec.Entrances) == 1 {
				url = fmt.Sprintf("%s.%s", app.Spec.Appid, zone)
			} else {
				url = fmt.Sprintf("%s%d.%s", app.Spec.Appid, i, zone)
			}
			urls = append(urls, url)

			entries[entrance.Name] = strings.Join(urls, ",")
		}
		vals["domain"] = entries

		chartName := fmt.Sprintf("./charts/%s", app.Spec.Name)

		if userspace.IsSysApp(app.Spec.Name) {
			chartName = fmt.Sprintf("./userapps/apps/%s", app.Spec.Name)
		}
		err = helm.UpgradeCharts(req.Request.Context(), actionConfig, cliSttings, app.Spec.Name, chartName,
			repoURL, app.Spec.Namespace, vals, true)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	}
	resp.WriteAsJson(appUpdated.Spec.Settings)
}

func (h *Handler) getAppEntrances(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}

	resp.WriteAsJson(app.Spec.Entrances)
}

func (h *Handler) getAppEntrancesSettings(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}
	resp.WriteAsJson(app.Spec.Settings)
}

func (h *Handler) getAppSettings(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}
	resp.WriteAsJson(app.Spec.Settings)
}

func getRepoURL(client *clientset.ClientSet, owner string) (string, error) {

	namespace := fmt.Sprintf("user-space-%s", owner)
	ep, err := client.KubeClient.Kubernetes().CoreV1().Endpoints(namespace).
		Get(context.TODO(), "appstore-service", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	repoURL := fmt.Sprintf("http://%s:82/charts", ep.Subsets[0].Addresses[0].IP)
	return repoURL, nil
}

func (h *Handler) setupAppAuthLevel(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}

	entranceName := req.PathParameter(ParamEntranceName)

	bodyData, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	var data map[string]map[string]string
	err = json.Unmarshal(bodyData, &data)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	appCopy := app.DeepCopy()
	entrances := appCopy.Spec.Entrances

	policy := make(map[string]map[string]interface{})
	err = json.Unmarshal([]byte(appCopy.Spec.Settings["policy"]), &policy)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	authLevel := data["authorizationLevel"]["authorization_level"]
	for i := range entrances {
		if entrances[i].Name == entranceName {
			if authLevel == constants.AuthorizationLevelOfPublic {
				policy[entrances[i].Name]["default_policy"] = constants.AuthorizationLevelOfPublic
			}
			if authLevel == constants.AuthorizationLevelOfPrivate &&
				entrances[i].AuthLevel == constants.AuthorizationLevelOfPublic {
				policy[entrances[i].Name]["default_policy"] = "two_factor"
			}
		}
	}

	for i := range entrances {
		if entrances[i].Name == entranceName {
			entrances[i].AuthLevel = authLevel
		}
	}

	appCopy.Spec.Entrances = entrances

	policyStr, err := json.Marshal(policy)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	appCopy.Spec.Settings["policy"] = string(policyStr)
	kclient := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)

	appUpdated, err := kclient.AppClient.AppV1alpha1().Applications().Update(req.Request.Context(), appCopy, metav1.UpdateOptions{})

	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteAsJson(appUpdated.Spec.Settings)
}

func (h *Handler) setupAppEntrancePolicy(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app, err)
		// if error, response in function. Do nothing
		return
	}

	entranceName := req.PathParameter(ParamEntranceName)

	bodyData, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	var data map[string]interface{}
	err = json.Unmarshal(bodyData, &data)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	settings := data["policy"].(map[string]interface{})

	appCopy := app.DeepCopy()

	var origin map[string]interface{}
	err = json.Unmarshal([]byte(appCopy.Spec.Settings["policy"]), &origin)

	merge := make(map[string]interface{})
	merge[entranceName] = settings

	for k, v := range origin {
		if k != entranceName {
			merge[k] = v.(map[string]interface{})
			continue
		}
	}
	settingsBytes, err := json.Marshal(merge)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"settings": map[string]string{
				"policy": string(settingsBytes),
			},
		},
	}
	patchByte, err := json.Marshal(patchData)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)

	appUpdated, err := client.AppClient.AppV1alpha1().Applications().Patch(req.Request.Context(), appCopy.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteAsJson(appUpdated.Spec.Settings)
}

func (h *Handler) tryToPatchDeploymentAnnotations(patchData map[string]interface{}, app *v1alpha1.Application) error {
	clientset, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	patchByte, err := json.Marshal(patchData)
	if err != nil {
		return err
	}
	deployment, err := clientset.AppsV1().Deployments(app.Spec.Namespace).
		Get(context.TODO(), app.Spec.DeploymentName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return h.tryToPatchStatefulSetAnnotations(patchData, app)
		}
		return err
	}
	a, err := clientset.AppsV1().Deployments(app.Spec.Namespace).
		Patch(context.TODO(), deployment.Name,
			types.MergePatchType,
			patchByte,
			metav1.PatchOptions{})
	klog.Infof("update annotations: %v", a.Annotations)
	return err
}

func (h *Handler) tryToPatchStatefulSetAnnotations(patchData map[string]interface{}, app *v1alpha1.Application) error {
	clientset, err := kubernetes.NewForConfig(h.kubeConfig)
	if err != nil {
		return err
	}
	patchByte, err := json.Marshal(patchData)
	if err != nil {
		return err
	}
	statefulSet, err := clientset.AppsV1().StatefulSets(app.Spec.Namespace).
		Get(context.TODO(), app.Spec.DeploymentName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err = clientset.AppsV1().StatefulSets(app.Spec.Namespace).
		Patch(context.TODO(), statefulSet.Name,
			types.MergePatchType,
			patchByte,
			metav1.PatchOptions{})

	return err
}
