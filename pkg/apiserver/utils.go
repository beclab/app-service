package apiserver

import (
	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/middlewareinstaller"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/workflowinstaller"
	"context"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	"github.com/pkg/errors"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
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

func getWorkflowConfigFromRepo(ctx context.Context, options *apputils.ConfigOptions) (*workflowinstaller.WorkflowConfig, error) {
	chartPath, err := apputils.GetIndexAndDownloadChart(ctx, options)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(chartPath + "/" + apputils.AppCfgFileName)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var cfg appcfg.AppConfiguration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	namespace, _ := utils.AppNamespace(options.App, options.Owner, cfg.Spec.Namespace)

	return &workflowinstaller.WorkflowConfig{
		WorkflowName: options.App,
		ChartsName:   chartPath,
		RepoURL:      options.RepoURL,
		Namespace:    namespace,
		OwnerName:    options.Owner,
		Cfg:          &cfg}, nil
}

func getMiddlewareConfigFromRepo(ctx context.Context, options *apputils.ConfigOptions) (*middlewareinstaller.MiddlewareConfig, error) {
	chartPath, err := apputils.GetIndexAndDownloadChart(ctx, options)
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

	var cfg appcfg.AppConfiguration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	namespace, _ := utils.AppNamespace(options.App, options.Owner, cfg.Spec.Namespace)

	return &middlewareinstaller.MiddlewareConfig{
		MiddlewareName: options.App,
		Title:          cfg.Metadata.Title,
		Version:        cfg.Metadata.Version,
		ChartsName:     chartPath,
		RepoURL:        options.RepoURL,
		Namespace:      namespace,
		OwnerName:      options.Owner,
		Cfg:            &cfg}, nil
}

type ListResult struct {
	Code   int   `json:"code"`
	Data   []any `json:"data"`
	Totals int   `json:"totals"`
}

func NewListResult[T any](items []T) ListResult {
	vs := make([]any, 0)
	if len(items) > 0 {
		for _, item := range items {
			vs = append(vs, item)
		}
	}
	return ListResult{Code: 200, Data: vs, Totals: len(items)}
}
