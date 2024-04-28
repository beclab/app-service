package tapr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/pkg/constants"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// MiddlewareType represents the type of middleware.
type MiddlewareType string

// describes the type of middleware support.
const (
	// TypePostgreSQL indicates the middleware is postgresql.
	TypePostgreSQL MiddlewareType = "postgres"
	// TypeMongoDB indicates the middleware is mongodb.
	TypeMongoDB MiddlewareType = "mongodb"
	// TypeRedis indicates the middleware is redis.
	TypeRedis MiddlewareType = "redis"
	// TypeZincSearch indicates the middleware is zinc search.
	TypeZincSearch MiddlewareType = "zinc"
)

func (mr MiddlewareType) String() string {
	return string(mr)
}

// MiddlewareReq represents a request for a middleware.
type MiddlewareReq struct {
	App          string         `json:"app"`
	AppNamespace string         `json:"appNamespace"`
	Namespace    string         `json:"namespace"`
	Middleware   MiddlewareType `json:"middleware"`
}

// MetaInfo represents middleware meta info.
type MetaInfo struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// MiddlewareRequestInfo contains information for middlewarerequest.
type MiddlewareRequestInfo struct {
	MetaInfo
	App       MetaInfo   `json:"app"`
	UserName  string     `json:"username,omitempty"`
	Password  string     `json:"password"`
	Type      string     `json:"type"`
	Databases []Database `json:"databases,omitempty"`
}

type MiddlewareRequestResp struct {
	MiddlewareRequestInfo
	Host      string            `json:"host"`
	Port      int32             `json:"port"`
	Indexes   map[string]string `json:"indexes"`
	Databases map[string]string `json:"databases"`
}

type Resp struct {
	Code int                    `json:"code"`
	Data *MiddlewareRequestResp `json:"data"`
}

// Apply middlewarerequest, get response and set values.
func Apply(middleware *Middleware, kubeConfig *rest.Config, appName, appNamespace,
	namespace, token, chartPath, ownerName string, vals map[string]interface{}) error {
	if middleware == nil {
		return nil
	}
	client := resty.New()
	client.SetRetryCount(3)
	client.SetRetryWaitTime(1 * time.Second)
	client.AddRetryCondition(func(r *resty.Response, err error) bool {
		statusCode := r.StatusCode()
		return statusCode == 404 || statusCode == 429 || statusCode >= 500
	})
	getMiddlewareRequest := func(middlewareType MiddlewareType) (*MiddlewareRequestResp, error) {
		url := "http://middleware-service.os-system/middleware/v1/request/info"
		request := MiddlewareReq{
			App:          appName,
			AppNamespace: appNamespace,
			Namespace:    namespace,
			Middleware:   middlewareType,
		}
		resp, err := client.SetTimeout(1*time.Second).R().
			SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
			SetHeader(constants.AuthorizationTokenKey, token).
			SetBody(request).Post(url)
		if err != nil {
			klog.Errorf("Failed to make middleware request middlewareType=%s err=%v", middlewareType, err)
			return nil, err
		}
		if resp.StatusCode() != 200 {
			klog.Errorf("Failed to get middleware request response status=%s body=%s", resp.Status(), resp.String())
			return nil, errors.New(resp.String())
		}
		var middlewareRequestResp Resp
		err = json.Unmarshal(resp.Body(), &middlewareRequestResp)
		if err != nil {
			klog.Errorf("Failed to unmarshal middleware request response err=%v", err)
			return nil, err
		}
		return middlewareRequestResp.Data, nil
	}

	if middleware.Postgres != nil {
		username := fmt.Sprintf("%s_%s_%s", middleware.Postgres.Username, ownerName, appName)
		username = strings.ReplaceAll(username, "-", "_")
		err := process(kubeConfig, appName, appNamespace, namespace, username,
			middleware.Postgres.Password, middleware.Postgres.Databases, TypePostgreSQL)
		if err != nil {
			return err
		}
		resp, err := getMiddlewareRequest(TypePostgreSQL)
		if err != nil {
			return err
		}
		vals["postgres"] = map[string]interface{}{
			"host":      resp.Host,
			"port":      resp.Port,
			"username":  resp.UserName,
			"password":  resp.Password,
			"databases": resp.Databases,
		}
	}

	if middleware.Redis != nil {
		username := ""
		err := process(kubeConfig, appName, appNamespace, namespace, username,
			middleware.Redis.Password, []Database{{Name: middleware.Redis.Namespace}}, TypeRedis)
		if err != nil {
			return err
		}
		resp, err := getMiddlewareRequest(TypeRedis)
		if err != nil {
			return err
		}
		vals["redis"] = map[string]interface{}{
			"host":       resp.Host,
			"port":       resp.Port,
			"password":   resp.Password,
			"namespaces": resp.Databases,
		}
	}

	if middleware.MongoDB != nil {
		username := fmt.Sprintf("%s-%s-%s", middleware.MongoDB.Username, ownerName, appName)
		err := process(kubeConfig, appName, appNamespace, namespace, username,
			middleware.MongoDB.Password, middleware.MongoDB.Databases, TypeMongoDB)
		if err != nil {
			return err
		}
		resp, err := getMiddlewareRequest(TypeMongoDB)
		if err != nil {
			return err
		}
		vals["mongodb"] = map[string]interface{}{
			"host":      resp.Host,
			"port":      resp.Port,
			"username":  resp.UserName,
			"password":  resp.Password,
			"databases": resp.Databases,
		}
	}
	if middleware.ZincSearch != nil {
		username := fmt.Sprintf("%s-%s-%s", middleware.ZincSearch.Username, ownerName, appName)
		err := setZincSearch(kubeConfig, appName, appNamespace, namespace, username,
			middleware.ZincSearch.Password, chartPath, middleware.ZincSearch.Indexes, TypeZincSearch)
		if err != nil {
			return err
		}
		resp, err := getMiddlewareRequest(TypeZincSearch)
		if err != nil {
			return err
		}
		vals["zinc"] = map[string]interface{}{
			"host":     resp.Host,
			"port":     resp.Port,
			"username": resp.UserName,
			"password": resp.Password,
			"indexes":  resp.Indexes,
		}
	}
	return nil
}

func process(kubeConfig *rest.Config, appName, appNamespace, namespace, username, password string,
	databases []Database, middleware MiddlewareType) error {
	request, err := GenMiddleRequest(middleware, appName,
		appNamespace, namespace, username, password, databases, []Index{})
	if err != nil {
		klog.Errorf("Failed to generate middleware request from template middlewareType=%s err=%v", middleware, err)
		return err
	}
	if len(password) == 0 {
		err = CreateOrUpdateSecret(kubeConfig, appName, namespace, middleware)
		if err != nil {
			return err
		}
	}
	_, err = CreateOrUpdateMiddlewareRequest(kubeConfig, namespace, request)
	if err != nil {
		klog.Info("Failed to create or update middleware request middlewareType=%s err=%v", middleware, err)
		return err
	}
	return nil
}

func setZincSearch(config *rest.Config, appName, appNameSpace, namespace, username, password,
	chartPath string, indexes []Index, middleware MiddlewareType) error {
	// create configmap before send mr request
	for _, index := range indexes {
		err := createOrUpdateConfigMapFromIndexes(config, index.Name, namespace, chartPath)
		if err != nil {
			return err
		}
	}

	request, err := GenMiddleRequest(middleware, appName, appNameSpace, namespace, username,
		password, []Database{}, indexes)
	if err != nil {
		klog.Info("Failed to generate middleware request middlewareType=%s err=%v", middleware, err)
		return err
	}
	if len(password) == 0 {
		err = CreateOrUpdateSecret(config, appName, namespace, middleware)
		if err != nil {
			return err
		}
	}
	_, err = CreateOrUpdateMiddlewareRequest(config, namespace, request)
	if err != nil {
		klog.Errorf("Failed to create middleware request middlewareType=%s err=%v", middleware, err)
		return err
	}
	return nil
}

func createOrUpdateConfigMapFromIndexes(config *rest.Config, name, namespace, chartPath string) error {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	f, err := os.Open(chartPath + "/" + name + ".json")
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	existConfigMap, err := client.CoreV1().ConfigMaps(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		// configmap does not exists, create new configmap
		if apierrors.IsNotFound(err) {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Data: map[string]string{
					"mappings": string(data),
				},
			}
			_, err = client.CoreV1().ConfigMaps(namespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		}
		return err
	}
	// configmap already exists, update data mappings
	existConfigMap.Data["mappings"] = string(data)
	_, err = client.CoreV1().ConfigMaps(namespace).Update(context.TODO(), existConfigMap, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}
