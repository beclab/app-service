package apiserver

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	v1alpha1client "bytetrade.io/web3os/app-service/pkg/client/clientset/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned/scheme"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"bytetrade.io/web3os/app-service/pkg/middlewareinstaller"
	"bytetrade.io/web3os/app-service/pkg/prometheus"
	"bytetrade.io/web3os/app-service/pkg/tapr"
	"bytetrade.io/web3os/app-service/pkg/upgrade"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"bytetrade.io/web3os/app-service/pkg/utils/files"
	"bytetrade.io/web3os/app-service/pkg/workflowinstaller"

	"github.com/Masterminds/semver/v3"
	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	"github.com/hashicorp/go-getter"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func getAppByName(req *restful.Request, resp *restful.Response) (*v1alpha1.Application, error) {
	appName := req.PathParameter(ParamAppName)
	owner := req.Attribute(constants.UserContextAttribute) // get owner from request token

	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)

	// run with request context for incoming client
	applist, err := client.AppClient.AppV1alpha1().Applications().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return nil, err
	}

	if applist.Items == nil || len(applist.Items) == 0 {
		api.HandleNotFound(resp, req, errors.New("there is not any application"))
		return nil, err
	}

	for _, app := range applist.Items {
		if app.Spec.Name == appName && app.Spec.Owner == owner {
			return &app, nil
		}
	}

	api.HandleNotFound(resp, req, fmt.Errorf("the application %s not found", appName))
	return nil, err
}

func checkVersionFormat(constraint string) error {
	_, err := semver.NewConstraint(constraint)
	if err != nil {
		return err
	}
	return nil
}

// CheckDependencies check application dependencies, returns unsatisfied dependency.
func CheckDependencies(ctx context.Context, deps []appinstaller.Dependency, ctrlClient client.Client, owner string, checkAll bool) ([]appinstaller.Dependency, error) {
	unSatisfiedDeps := make([]appinstaller.Dependency, 0)
	client, err := utils.GetClient()
	if err != nil {
		return unSatisfiedDeps, err
	}

	applist, err := client.AppV1alpha1().Applications().List(ctx, metav1.ListOptions{})
	if err != nil {
		return unSatisfiedDeps, err
	}
	appToVersion := make(map[string]string)
	appNames := make([]string, len(applist.Items))
	for _, app := range applist.Items {
		clusterScoped, _ := strconv.ParseBool(app.Spec.Settings["clusterScoped"])
		// add app name to list if app is cluster scoped or owner equal app.Spec.Name
		if clusterScoped || owner == app.Spec.Owner {
			appNames = append(appNames, app.Spec.Name)
			appToVersion[app.Spec.Name] = app.Spec.Settings["version"]
		}
	}
	set := sets.NewString(appNames...)

	for _, dep := range deps {
		if dep.Type == constants.DependencyTypeSystem {
			terminus, err := upgrade.GetTerminusVersion(ctx, ctrlClient)
			if err != nil {
				return unSatisfiedDeps, err
			}

			if !utils.MatchVersion(terminus.Spec.Version, dep.Version) {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("terminus version %s not match dependency %s", terminus.Spec.Version, dep.Version)
				}
			}
		}
		if dep.Type == constants.DependencyTypeApp {
			if !set.Has(dep.Name) {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("dependency application %s not existed", dep.Name)
				}
			}
			if !utils.MatchVersion(appToVersion[dep.Name], dep.Version) {
				unSatisfiedDeps = append(unSatisfiedDeps, dep)
				if !checkAll {
					return unSatisfiedDeps, fmt.Errorf("%s version: %s not match dependency %s", dep.Name, appToVersion[dep.Name], dep.Version)
				}
			}
		}
	}
	if len(unSatisfiedDeps) > 0 {
		return unSatisfiedDeps, fmt.Errorf("some dependency not satisfied")
	}
	return unSatisfiedDeps, nil
}

// CheckAppRequirement check if the cluster has enough resources for application install/upgrade.
func CheckAppRequirement(kubeConfig *rest.Config, token string, appConfig *appinstaller.ApplicationConfig) (string, error) {
	metrics, _, err := GetClusterResource(kubeConfig, token)
	if err != nil {
		return "", err
	}

	klog.Infof("Current resource=%s", utils.PrettyJSON(metrics))
	klog.Infof("App required resource=%s", utils.PrettyJSON(appConfig.Requirement))

	switch {
	case appConfig.Requirement.Disk != nil &&
		appConfig.Requirement.Disk.CmpInt64(int64(metrics.Disk.Total-metrics.Disk.Usage)) > 0:
		return "disk", errors.New("The app's DISK requirement cannot be satisfied")
	case appConfig.Requirement.Memory != nil &&
		appConfig.Requirement.Memory.CmpInt64(int64(metrics.Memory.Total*0.9-metrics.Memory.Usage)) > 0:
		return "memory", errors.New("The app's MEMORY requirement cannot be satisfied")
	case appConfig.Requirement.CPU != nil:
		availableCPU, _ := resource.ParseQuantity(strconv.FormatFloat(metrics.CPU.Total*0.9-metrics.CPU.Usage, 'f', -1, 64))
		if appConfig.Requirement.CPU.Cmp(availableCPU) > 0 {
			return "cpu", errors.New("The app's CPU requirement cannot be satisfied")
		}
	case appConfig.Requirement.GPU != nil && !appConfig.Requirement.GPU.IsZero() &&
		metrics.GPU.Total <= 0:
		return "gpu", errors.New("The app's GPU requirement cannot be satisfied")
	}

	allocatedResources, err := getRequestResources()
	if err != nil {
		return "", err
	}
	if len(allocatedResources) == 1 {
		sufficientCPU, sufficientMemory := false, false
		if appConfig.Requirement.CPU == nil {
			sufficientCPU = true
		}
		if appConfig.Requirement.Memory == nil {
			sufficientMemory = true
		}
		for _, v := range allocatedResources {
			if appConfig.Requirement.CPU != nil {
				if v.cpu.allocatable.Cmp(*appConfig.Requirement.CPU) > 0 {
					sufficientCPU = true
				}
			}
			if appConfig.Requirement.Memory != nil {
				if v.memory.allocatable.Cmp(*appConfig.Requirement.Memory) > 0 {
					sufficientMemory = true
				}
			}
		}
		if !sufficientCPU {
			return "cpu", errors.New("The app's CPU requirement specified in the kubernetes requests cannot be satisfied")
		}
		if !sufficientMemory {
			return "memory", errors.New("The app's MEMORY requirement specified in the kubernetes requests cannot be satisfied")
		}
	}

	return "", nil
}

// CheckUserResRequirement check if the user has enough resources for application install/upgrade.
func CheckUserResRequirement(ctx context.Context, kubeConfig *rest.Config, appConfig *appinstaller.ApplicationConfig, username string) (string, error) {
	metrics, err := prometheus.GetCurUserResource(ctx, kubeConfig, username)
	if err != nil {
		return "", err
	}
	switch {
	case appConfig.Requirement.Memory != nil && metrics.Memory.Total != 0 &&
		appConfig.Requirement.Memory.CmpInt64(int64(metrics.Memory.Total*0.9-metrics.Memory.Usage)) > 0:
		return "memory", errors.New("The user's app MEMORY requirement cannot be satisfied")
	case appConfig.Requirement.CPU != nil && metrics.CPU.Total != 0:
		availableCPU, _ := resource.ParseQuantity(strconv.FormatFloat(metrics.CPU.Total*0.9-metrics.CPU.Usage, 'f', -1, 64))
		if appConfig.Requirement.CPU.Cmp(availableCPU) > 0 {
			return "cpu", errors.New("The user's app CPU requirement cannot be satisfied")
		}
	}
	return "", nil
}

// GetClusterResource returns cluster resource metrics and cluster arches.
func GetClusterResource(kubeConfig *rest.Config, token string) (*prometheus.ClusterMetrics, []string, error) {
	supportArch := make([]string, 0)
	arches := sets.String{}

	config := rest.Config{
		Host:        constants.KubeSphereAPIHost,
		BearerToken: token,
		APIPath:     "/kapis",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &schema.GroupVersion{
				Group:   "monitoring.kubesphere.io",
				Version: "v1alpha3",
			},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, supportArch, err
	}

	metricParam := "cluster_cpu_usage|cluster_cpu_total|cluster_memory_usage_wo_cache|cluster_memory_total|cluster_disk_size_usage|cluster_disk_size_capacity|cluster_pod_running_count|cluster_pod_quota$"

	client.Client.Timeout = 2 * time.Second
	res := client.Get().Resource("cluster").
		Param("metrics_filter", metricParam).Do(context.TODO())

	if res.Error() != nil {
		return nil, supportArch, res.Error()
	}

	var metrics Metrics
	data, err := res.Raw()
	if err != nil {
		return nil, supportArch, err
	}

	err = json.Unmarshal(data, &metrics)
	if err != nil {
		return nil, supportArch, err
	}

	var clusterMetrics prometheus.ClusterMetrics
	for _, m := range metrics.Results {
		switch m.MetricName {
		case "cluster_cpu_usage":
			clusterMetrics.CPU.Usage = getValue(&m)
		case "cluster_cpu_total":
			clusterMetrics.CPU.Total = getValue(&m)

		case "cluster_disk_size_usage":
			clusterMetrics.Disk.Usage = getValue(&m)
		case "cluster_disk_size_capacity":
			clusterMetrics.Disk.Total = getValue(&m)

		case "cluster_memory_total":
			clusterMetrics.Memory.Total = getValue(&m)
		case "cluster_memory_usage_wo_cache":
			clusterMetrics.Memory.Usage = getValue(&m)
		}
	}

	// get k8s client with node list privileges
	k8sClient, err := v1alpha1client.NewKubeClient("", kubeConfig)
	if err != nil {
		klog.Errorf("Failed to create k8s client err=%v", err)
	} else {
		nodes, err := k8sClient.Kubernetes().CoreV1().Nodes().List(
			context.TODO(),
			metav1.ListOptions{},
		)

		if err != nil && !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to list node err=%v", err)
		}

		if apierrors.IsNotFound(err) {
			clusterMetrics.GPU.Total = 0
		} else {
			var total float64 = 0
			for _, n := range nodes.Items {
				arches.Insert(n.Labels["kubernetes.io/arch"])
				if quantity, ok := n.Status.Capacity[constants.NvidiaGPU]; ok {
					total += quantity.AsApproximateFloat64()
				} else if quantity, ok = n.Status.Capacity[constants.VirtAiTechVGPU]; ok {
					total += quantity.AsApproximateFloat64()
				}
			}

			clusterMetrics.GPU.Total = total
		}

	}
	for arch := range arches {
		supportArch = append(supportArch, arch)
	}
	return &clusterMetrics, supportArch, nil
}

func getValue(m *kubesphere.Metric) float64 {
	return m.MetricData.MetricValues[0].Sample[1]
}
func isInPrivateNamespace(namespace string) bool {
	privateNamespaces := []string{"os-system", "user-space-", "user-system-"}
	for _, ns := range privateNamespaces {
		if strings.HasPrefix(namespace, ns) {
			return true
		}
	}
	return false
}

type resources struct {
	cpu    usage
	memory usage
}

type usage struct {
	allocatable *resource.Quantity
}

func getRequestResources() (map[string]resources, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	allocatedResources := make(map[string]resources)
	for _, node := range nodes.Items {
		allocatedResources[node.Name] = resources{cpu: usage{allocatable: node.Status.Allocatable.Cpu()},
			memory: usage{allocatable: node.Status.Allocatable.Memory()}}
		fieldSelector := fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", node.Name)
		pods, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
			FieldSelector: fieldSelector,
		})
		if err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.Containers {
				allocatedResources[node.Name].cpu.allocatable.Sub(*container.Resources.Requests.Cpu())
				allocatedResources[node.Name].memory.allocatable.Sub(*container.Resources.Requests.Memory())
			}
		}
	}
	return allocatedResources, nil
}

func genTerminusNonce() (string, error) {
	randomKey := os.Getenv("APP_RANDOM_KEY")
	timestamp := getTimestamp()
	cipherText, err := aesEncrypt([]byte(timestamp), []byte(randomKey))
	if err != nil {
		return "", err
	}
	b64CipherText := base64.StdEncoding.EncodeToString(cipherText)
	terminusNonce := "appservice:" + b64CipherText
	return terminusNonce, nil
}

func getTimestamp() string {
	t := time.Now().Unix()
	return strconv.Itoa(int(t))
}

func aesEncrypt(origin, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	origin = pKCS7Padding(origin, blockSize)
	blockMode := cipher.NewCBCEncrypter(block, key[:blockSize])
	crypted := make([]byte, len(origin))
	blockMode.CryptBlocks(crypted, origin)
	return crypted, nil
}

func pKCS7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

func getWorkflowConfigFromRepo(ctx context.Context, owner, app, repoURL, version, token string) (*workflowinstaller.WorkflowConfig, error) {
	chartPath, err := GetIndexAndDownloadChart(ctx, app, repoURL, version, token)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(chartPath + "/" + AppCfgFileName)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var cfg appinstaller.AppConfiguration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	namespace, _ := utils.AppNamespace(app, owner, cfg.Spec.Namespace)

	return &workflowinstaller.WorkflowConfig{
		WorkflowName: app,
		ChartsName:   chartPath,
		RepoURL:      repoURL,
		Namespace:    namespace,
		OwnerName:    owner,
		Cfg:          &cfg}, nil
}

// GetIndexAndDownloadChart download a chart and returns download chart path.
func GetIndexAndDownloadChart(ctx context.Context, app, repoURL, version, token string) (string, error) {
	terminusNonce, err := genTerminusNonce()
	if err != nil {
		return "", err
	}
	client := resty.New().SetTimeout(2*time.Second).
		SetHeader(constants.AuthorizationTokenKey, token).
		SetHeader("Terminus-Nonce", terminusNonce)
	indexFileURL := repoURL
	if repoURL[len(repoURL)-1] != '/' {
		indexFileURL += "/"
	}
	indexFileURL += "index.yaml"
	resp, err := client.R().Get(indexFileURL)
	if err != nil {
		return "", err
	}

	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("get app config from repo returns unexpected status code, %d", resp.StatusCode())
	}

	index, err := loadIndex(resp.Body())
	if err != nil {
		klog.Errorf("Failed to load chart index err=%v", err)
		return "", err
	}

	klog.Infof("Success to find app chart from index app=%s version=%s", app, version)
	// get specified version chart, if version is empty return the chart with the latest stable version
	chartVersion, err := index.Get(app, version)

	if err != nil {
		klog.Errorf("Failed to get chart version err=%v", err)
		return "", fmt.Errorf("app [%s-%s] not found in repo", app, version)
	}

	chartURL, err := repo.ResolveReferenceURL(repoURL, chartVersion.URLs[0])
	if err != nil {
		return "", err
	}

	url, err := url.Parse(chartURL)
	if err != nil {
		return "", err
	}

	// assume the chart path is app name
	chartPath := appinstaller.ChartsPath + "/" + app
	if files.IsExist(chartPath) {
		if err := files.RemoveAll(chartPath); err != nil {
			return "", err
		}
	}
	_, err = downloadAndUnpack(ctx, url, token, terminusNonce)
	if err != nil {
		return "", err
	}
	return chartPath, nil
}

func downloadAndUnpack(ctx context.Context, tgz *url.URL, token, terminusNonce string) (string, error) {
	dst := appinstaller.ChartsPath
	g := new(getter.HttpGetter)
	g.Header = make(http.Header)
	g.Header.Set(constants.AuthorizationTokenKey, token)
	g.Header.Set("Terminus-Nonce", terminusNonce)
	downloader := &getter.Client{
		Ctx:       ctx,
		Dst:       dst,
		Src:       tgz.String(),
		Mode:      getter.ClientModeDir,
		Detectors: getter.Detectors,
		Getters: map[string]getter.Getter{
			"http": g,
			"file": new(getter.FileGetter),
		},
	}

	//download the files
	if err := downloader.Get(); err != nil {
		klog.Errorf("Failed to get path=%s err=%v", downloader.Src, err)
		return "", err
	}

	return dst, nil
}

func loadIndex(data []byte) (*repo.IndexFile, error) {
	i := &repo.IndexFile{}

	if len(data) == 0 {
		return i, repo.ErrEmptyIndexYaml
	}

	if err := yaml.UnmarshalStrict(data, i); err != nil {
		return i, err
	}

	for name, cvs := range i.Entries {
		for idx := len(cvs) - 1; idx >= 0; idx-- {
			if cvs[idx].APIVersion == "" {
				cvs[idx].APIVersion = chart.APIVersionV1
			}
			if err := cvs[idx].Validate(); err != nil {
				klog.Infof("Skipping loading invalid entry for chart name=%q version=%q err=%v", name, cvs[idx].Version, err)
				cvs = append(cvs[:idx], cvs[idx+1:]...)
			}
		}
	}
	i.SortEntries()
	if i.APIVersion == "" {
		return i, repo.ErrNoAPIVersion
	}
	return i, nil
}

func getMiddlewareConfigFromRepo(ctx context.Context, owner, app, repoURL, version, token string) (*middlewareinstaller.MiddlewareConfig, error) {
	chartPath, err := GetIndexAndDownloadChart(ctx, app, repoURL, version, token)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(chartPath + "/OlaresManifest.yaml")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var cfg appinstaller.AppConfiguration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	namespace, _ := utils.AppNamespace(app, owner, cfg.Spec.Namespace)

	return &middlewareinstaller.MiddlewareConfig{
		MiddlewareName: app,
		Title:          cfg.Metadata.Title,
		Version:        cfg.Metadata.Version,
		ChartsName:     chartPath,
		RepoURL:        repoURL,
		Namespace:      namespace,
		OwnerName:      owner,
		Cfg:            &cfg}, nil
}

func CheckMiddlewareRequirement(ctx context.Context, kubeConfig *rest.Config, middleware *tapr.Middleware) (bool, error) {
	if middleware != nil && middleware.MongoDB != nil {
		dConfig, err := dynamic.NewForConfig(kubeConfig)
		if err != nil {
			return false, err
		}
		dClient, err := middlewareinstaller.NewMiddlewareMongodb(dConfig)
		if err != nil {
			return false, err
		}
		u, err := dClient.Get(ctx, "os-system", "mongo-cluster", metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if u == nil {
			return false, nil
		}
		state, _, err := unstructured.NestedString(u.Object, "status", "state")
		if err != nil {
			return false, err
		}
		if state == "ready" {
			return true, nil
		}
		return false, nil
	}
	return true, nil
}
