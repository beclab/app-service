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
	"bytetrade.io/web3os/app-service/pkg/provider"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"

	"github.com/emicklei/go-restful/v3"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func (h *Handler) setupApp(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
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
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
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
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
		// if error, response in function. Do nothing
		return
	}

	resp.WriteAsJson(app.Spec.Entrances)
}

func (h *Handler) getAppEntrancesSettings(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
		// if error, response in function. Do nothing
		return
	}
	resp.WriteAsJson(app.Spec.Settings)
}

func (h *Handler) getAppSettings(req *restful.Request, resp *restful.Response) {
	app, err := getAppByName(req, resp)
	if err != nil {
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
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
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
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
				policy[entrances[i].Name]["default_policy"] = "system"
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
		klog.Errorf("Failed to get app name=%s err=%v", app.Spec.Name, err)
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

type permission struct {
	DataType string   `json:"dataType"`
	Group    string   `json:"group"`
	Version  string   `json:"version"`
	Ops      []string `json:"ops"`
}

type applicationPermission struct {
	App         string       `json:"app"`
	Owner       string       `json:"owner"`
	Permissions []permission `json:"permissions"`
}

func (h *Handler) applicationPermissionList(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute).(string)
	//token := req.HeaderParameter(constants.AuthorizationTokenKey)
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ret := make([]applicationPermission, 0)
	apClient := provider.NewApplicationPermissionRequest(client)
	aps, err := apClient.List(req.Request.Context(), metav1.NamespaceAll, metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, ap := range aps.Items {
		if ap.Object == nil {
			continue
		}
		app, _, _ := unstructured.NestedString(ap.Object, "spec", "app")
		perms, _, _ := unstructured.NestedSlice(ap.Object, "spec", "permissions")
		klog.Infof("perms Type: %T, perms: %v", perms, perms)
		permissions := make([]permission, 0)
		for _, p := range perms {
			if perm, ok := p.(map[string]interface{}); ok {
				ops := make([]string, 0)
				for _, op := range perm["ops"].([]interface{}) {
					if opStr, ok := op.(string); ok {
						ops = append(ops, opStr)
					}
				}
				permissions = append(permissions, permission{
					DataType: perm["dataType"].(string),
					Group:    perm["group"].(string),
					Version:  perm["version"].(string),
					Ops:      ops,
				})
			}

		}
		ret = append(ret, applicationPermission{
			App:         app,
			Owner:       owner,
			Permissions: permissions,
		})
	}
	resp.WriteAsJson(ret)
}

func (h *Handler) getApplicationPermission(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var ret *applicationPermission
	apClient := provider.NewApplicationPermissionRequest(client)
	namespace := fmt.Sprintf("user-system-%s", owner)
	aps, err := apClient.List(req.Request.Context(), namespace, metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, ap := range aps.Items {
		if ap.Object == nil {
			continue
		}
		appName, _, _ := unstructured.NestedString(ap.Object, "spec", "app")
		if appName == app {
			perms, _, _ := unstructured.NestedSlice(ap.Object, "spec", "permissions")
			permissions := make([]permission, 0)
			for _, p := range perms {
				if perm, ok := p.(map[string]interface{}); ok {
					ops := make([]string, 0)
					for _, op := range perm["ops"].([]interface{}) {
						if opStr, ok := op.(string); ok {
							ops = append(ops, opStr)
						}
					}
					permissions = append(permissions, permission{
						DataType: perm["dataType"].(string),
						Group:    perm["group"].(string),
						Version:  perm["version"].(string),
						Ops:      ops,
					})
				}

			}
			ret = &applicationPermission{
				App:         appName,
				Owner:       owner,
				Permissions: permissions,
			}
			break
		}
	}
	if ret == nil {
		api.HandleNotFound(resp, req, errors.New("application permission not found"))
		return
	}
	resp.WriteAsJson(ret)
}

type providerRegistry struct {
	DataType    string  `json:"dataType"`
	Deployment  string  `json:"deployment"`
	Description string  `json:"description"`
	Endpoint    string  `json:"endpoint"`
	Group       string  `json:"group"`
	Kind        string  `json:"kind"`
	Namespace   string  `json:"namespace"`
	OpApis      []opApi `json:"opApis"`
	Version     string  `json:"version"`
}

type opApi struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

func (h *Handler) getProviderRegistry(req *restful.Request, resp *restful.Response) {
	dataTypeReq := req.PathParameter(ParamDataType)
	groupReq := req.PathParameter(ParamGroup)
	versionReq := req.PathParameter(ParamVersion)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	var ret *providerRegistry
	rClient := provider.NewRegistryRequest(client)
	namespace := fmt.Sprintf("user-system-%s", owner)
	prs, err := rClient.List(req.Request.Context(), namespace, metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, ap := range prs.Items {
		if ap.Object == nil {
			continue
		}
		dataType, _, _ := unstructured.NestedString(ap.Object, "spec", "dataType")
		group, _, _ := unstructured.NestedString(ap.Object, "spec", "group")
		version, _, _ := unstructured.NestedString(ap.Object, "spec", "version")
		kind, _, _ := unstructured.NestedString(ap.Object, "spec", "kind")

		if dataType == dataTypeReq && group == groupReq && version == versionReq && kind == "provider" {
			deployment, _, _ := unstructured.NestedString(ap.Object, "spec", "deployment")
			description, _, _ := unstructured.NestedString(ap.Object, "spec", "description")
			endpoint, _, _ := unstructured.NestedString(ap.Object, "spec", "endpoint")
			ns, _, _ := unstructured.NestedString(ap.Object, "spec", "namespace")
			opApis := make([]opApi, 0)
			opApiList, _, _ := unstructured.NestedSlice(ap.Object, "spec", "opApis")
			for _, op := range opApiList {
				if aop, ok := op.(map[string]interface{}); ok {
					opApis = append(opApis, opApi{
						Name: aop["name"].(string),
						URI:  aop["uri"].(string),
					})
				}
			}
			ret = &providerRegistry{
				DataType:    dataType,
				Deployment:  deployment,
				Description: description,
				Endpoint:    endpoint,
				Kind:        kind,
				Group:       group,
				Namespace:   ns,
				OpApis:      opApis,
				Version:     version,
			}
			break
		}
	}
	if ret == nil {
		api.HandleNotFound(resp, req, errors.New("provider registry not found"))
		return
	}
	resp.WriteAsJson(ret)
}

func (h *Handler) getApplicationProviderList(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute).(string)
	app := req.PathParameter(ParamAppName)
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ret := make([]providerRegistry, 0)
	rClient := provider.NewRegistryRequest(client)
	namespace := fmt.Sprintf("user-system-%s", owner)
	prs, err := rClient.List(req.Request.Context(), namespace, metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	for _, ap := range prs.Items {
		if ap.Object == nil {
			continue
		}
		deployment, _, _ := unstructured.NestedString(ap.Object, "spec", "deployment")
		kind, _, _ := unstructured.NestedString(ap.Object, "spec", "kind")

		if app == deployment && kind == "provider" {
			dataType, _, _ := unstructured.NestedString(ap.Object, "spec", "dataType")
			group, _, _ := unstructured.NestedString(ap.Object, "spec", "group")
			description, _, _ := unstructured.NestedString(ap.Object, "spec", "description")
			endpoint, _, _ := unstructured.NestedString(ap.Object, "spec", "endpoint")
			ns, _, _ := unstructured.NestedString(ap.Object, "spec", "namespace")
			version, _, _ := unstructured.NestedString(ap.Object, "spec", "version")
			opApis := make([]opApi, 0)
			opApiList, _, _ := unstructured.NestedSlice(ap.Object, "spec", "opApis")
			for _, op := range opApiList {
				if aop, ok := op.(map[string]interface{}); ok {
					opApis = append(opApis, opApi{
						Name: aop["name"].(string),
						URI:  aop["uri"].(string),
					})
				}
			}
			ret = append(ret, providerRegistry{
				DataType:    dataType,
				Deployment:  deployment,
				Description: description,
				Endpoint:    endpoint,
				Kind:        kind,
				Group:       group,
				Namespace:   ns,
				OpApis:      opApis,
				Version:     version,
			})

		}
	}
	resp.WriteAsJson(ret)
}

func (h *Handler) getApplicationSubject(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	client, err := dynamic.NewForConfig(h.kubeConfig)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	dc, err := tapr.NewMiddlewareRequest(client)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	namespace := fmt.Sprintf("user-system-%s", owner)
	mrs, err := dc.List(req.Request.Context(), namespace, metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ret := make([]tapr.NatsConfig, 0)
	klog.Infof("get Application Subject...............")
	klog.Infof("mrs.Items:len: %v", len(mrs.Items))
	if len(mrs.Items) > 0 {
		for _, mr := range mrs.Items {
			if mr.Object == nil {
				continue
			}
			middlewareType, _, _ := unstructured.NestedString(mr.Object, "spec", "middleware")
			klog.Infof("middlewareType: %v", middlewareType)
			if middlewareType != "nats" {
				continue
			}
			appName, _, _ := unstructured.NestedString(mr.Object, "spec", "app")
			if appName != app {
				continue
			}
			username, _, _ := unstructured.NestedString(mr.Object, "spec", "nats", "user")

			klog.Infof("appName: %v", appName)
			natsCfg := tapr.NatsConfig{}
			natsCfg.Username = username
			nats, _, _ := unstructured.NestedMap(mr.Object, "spec", "nats")
			subjects, _, _ := unstructured.NestedSlice(nats, "subjects")
			klog.Infof("subjects: %v", subjects)
			natsCfg.Subjects = make([]tapr.Subject, 0)
			for _, s := range subjects {
				subject := tapr.Subject{}
				subjectMap := s.(map[string]interface{})
				subject.Name, _, _ = unstructured.NestedString(subjectMap, "name")

				permission, _, _ := unstructured.NestedMap(subjectMap, "permission")
				subject.Permission = tapr.Permission{
					Pub: permission["pub"].(string),
					Sub: permission["sub"].(string),
				}
				subject.Export = make([]tapr.Permission, 0)
				export, found, _ := unstructured.NestedSlice(subjectMap, "export")
				if found {
					for _, e := range export {
						exportMap := e.(map[string]interface{})
						subject.Export = append(subject.Export,
							tapr.Permission{
								AppName: exportMap["appName"].(string),
								Pub:     exportMap["pub"].(string),
								Sub:     exportMap["sub"].(string),
							},
						)
					}
				}
				natsCfg.Subjects = append(natsCfg.Subjects, subject)
			}
			natsCfg.Refs = make([]tapr.Ref, 0)
			refs, _, _ := unstructured.NestedSlice(nats, "refs")
			for _, r := range refs {
				ref := tapr.Ref{}
				refMap := r.(map[string]interface{})
				ref.AppName, _, _ = unstructured.NestedString(refMap, "appName")
				ref.AppNamespace, _, _ = unstructured.NestedString(refMap, "appNamespace")

				refSubjects, _, _ := unstructured.NestedSlice(refMap, "subjects")
				for _, rs := range refSubjects {
					refSubject := tapr.RefSubject{}
					rsMap := rs.(map[string]interface{})
					refSubject.Name, _, _ = unstructured.NestedString(rsMap, "name")
					refSubject.Perm, _, _ = unstructured.NestedStringSlice(rsMap, "perm")
					ref.Subjects = append(ref.Subjects, refSubject)
				}

				natsCfg.Refs = append(natsCfg.Refs, ref)
			}
			ret = append(ret, natsCfg)
		}
	}
	resp.WriteAsJson(ret)
}
