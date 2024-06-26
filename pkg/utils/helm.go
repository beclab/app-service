package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"

	refdocker "github.com/containerd/containerd/reference/docker"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmLoader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
)

func actionConfig() (*action.Configuration, error) {
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}
	configuration := action.Configuration{
		Releases:       storage.Init(driver.NewMemory()),
		KubeClient:     &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: ioutil.Discard}},
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
	}
	return &configuration, nil
}

func InitAction() (*action.Install, error) {
	config, err := actionConfig()
	if err != nil {
		klog.Infof("actionConfig err=%v", err)
		return nil, err
	}
	instAction := action.NewInstall(config)
	instAction.Namespace = "spaced"
	instAction.ReleaseName = "test-release"
	instAction.DryRun = true
	return instAction, nil
}

func getChart(instAction *action.Install, filepath string) (*chart.Chart, error) {
	cp, err := instAction.ChartPathOptions.LocateChart(filepath, &cli.EnvSettings{})
	if err != nil {
		klog.Infof("locate chart error: %v", err)
		return nil, err
	}
	p := getter.All(&cli.EnvSettings{})
	chartRequested, err := helmLoader.Load(cp)
	if err != nil {
		klog.Infof("Load err=%v", err)
		return nil, err
	}
	if req := chartRequested.Metadata.Dependencies; req != nil {
		if err := action.CheckDependencies(chartRequested, req); err != nil {
			if instAction.DependencyUpdate {
				man := &downloader.Manager{
					ChartPath:  cp,
					Keyring:    instAction.ChartPathOptions.Keyring,
					SkipUpdate: false,
					Getters:    p,
				}
				if err := man.Update(); err != nil {
					return nil, err
				}
				// Reload the chart with the updated Chart.lock file.
				if chartRequested, err = helmLoader.Load(cp); err != nil {
					return nil, err
				}
			} else {

			}
		}
	}
	return chartRequested, nil
}

func GetResourceListFromChart(chartPath string) (resources kube.ResourceList, err error) {
	instAction, err := InitAction()
	if err != nil {
		return nil, err
	}
	instAction.Namespace = "app-namespace"
	chartRequested, err := getChart(instAction, chartPath)
	if err != nil {
		klog.Infof("getchart err=%v", err)
		return nil, err
	}

	// fake values for helm dry run
	values := make(map[string]interface{})
	values["bfl"] = map[string]interface{}{
		"username": "bfl-username",
	}
	values["user"] = map[string]interface{}{
		"zone": "user-zone",
	}
	values["schedule"] = map[string]interface{}{
		"nodeName": "node",
	}
	values["userspace"] = map[string]interface{}{
		"appCache": "appcache",
		"userData": "userspace/Home",
	}
	values["os"] = map[string]interface{}{
		"appKey":    "appKey",
		"appSecret": "appSecret",
	}

	values["domain"] = map[string]string{}
	values["dep"] = map[string]interface{}{}
	values["postgres"] = map[string]interface{}{
		"databases": map[string]interface{}{},
	}
	values["redis"] = map[string]interface{}{}
	values["mongodb"] = map[string]interface{}{
		"databases": map[string]interface{}{},
	}
	values["zinc"] = map[string]interface{}{
		"indexes": map[string]interface{}{},
	}
	values["svcs"] = map[string]interface{}{}

	ret, err := instAction.RunWithContext(context.Background(), chartRequested, values)
	if err != nil {
		return nil, err
	}

	var metadataAccessor = meta.NewAccessor()
	d := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(ret.Manifest), 4096)
	for {
		ext := runtime.RawExtension{}
		if err := d.Decode(&ext); err != nil {
			if err == io.EOF {
				return resources, nil
			}
			return nil, fmt.Errorf("error parsing")
		}
		ext.Raw = bytes.TrimSpace(ext.Raw)
		if len(ext.Raw) == 0 || bytes.Equal(ext.Raw, []byte("null")) {
			continue
		}
		obj, _, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
		if err != nil {
			return nil, err
		}
		name, _ := metadataAccessor.Name(obj)
		namespace, _ := metadataAccessor.Namespace(obj)
		info := &resource.Info{
			Namespace: namespace,
			Name:      name,
			Object:    obj,
		}
		resources = append(resources, info)
	}
}

func GetRefFromResourceList(chartPath string) (refs []string, err error) {
	resources, err := GetResourceListFromChart(chartPath)
	if err != nil {
		klog.Infof("get resourcelist from chart err=%v", err)
		return refs, err
	}
	seen := make(map[string]struct{})
	for _, r := range resources {
		kind := r.Object.GetObjectKind().GroupVersionKind().Kind
		if kind == "Deployment" {
			var deployment v1.Deployment
			err = scheme.Scheme.Convert(r.Object, &deployment, nil)
			if err != nil {
				return refs, err
			}
			for _, c := range deployment.Spec.Template.Spec.InitContainers {
				refs = append(refs, c.Image)
			}
			for _, c := range deployment.Spec.Template.Spec.Containers {
				refs = append(refs, c.Image)
			}
		}
		if kind == "StatefulSet" {
			var sts v1.StatefulSet
			err = scheme.Scheme.Convert(r.Object, &sts, nil)
			if err != nil {
				return refs, err
			}
			for _, c := range sts.Spec.Template.Spec.InitContainers {
				refs = append(refs, c.Image)
			}
			for _, c := range sts.Spec.Template.Spec.Containers {
				refs = append(refs, c.Image)
			}
		}
	}
	filteredRefs := make([]string, 0)
	for _, ref := range refs {
		if _, ok := seen[ref]; !ok {
			named, _ := refdocker.ParseDockerRef(ref)
			filteredRefs = append(filteredRefs, named.String())
			seen[named.String()] = struct{}{}
		}
	}
	return filteredRefs, nil
}
