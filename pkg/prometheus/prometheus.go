package prometheus

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"bytetrade.io/web3os/app-service/pkg/kubesphere"

	"github.com/prometheus/client_golang/api"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
)

const MeteringDefaultTimeout = 20 * time.Second
const Endpoint = "http://prometheus-operated.kubesphere-monitoring-system.svc:9090"

type Level int

const (
	LevelCluster = 1 << iota
	LevelUser
)

type QueryOptions struct {
	Level    Level
	UserName string
}

type Monitoring interface {
	GetNamedMetrics(ctx context.Context, metrics []string, ts time.Time, opts QueryOptions) []Metric
}

// prometheus implements monitoring interface backed by Prometheus
type prometheus struct {
	client apiv1.API
}

func New(address string) (Monitoring, error) {
	cfg := api.Config{
		Address: address,
	}

	client, err := api.NewClient(cfg)
	return prometheus{client: apiv1.NewAPI(client)}, err
}

func (p prometheus) GetNamedMetrics(ctx context.Context, metrics []string, ts time.Time, opts QueryOptions) []Metric {
	var res []Metric
	var mtx sync.Mutex
	var wg sync.WaitGroup

	for _, metric := range metrics {
		wg.Add(1)
		go func(metric string) {
			parsedResp := Metric{MetricName: metric}

			value, _, err := p.client.Query(ctx, makeExpr(metric, opts), ts)
			if err != nil {
				parsedResp.Error = err.Error()
			} else {
				parsedResp.MetricData = parseQueryResp(value)
			}

			mtx.Lock()
			res = append(res, parsedResp)
			mtx.Unlock()

			wg.Done()
		}(metric)
	}

	wg.Wait()

	return res
}

func parseQueryResp(value model.Value) MetricData {
	res := MetricData{MetricType: MetricTypeVector}

	data, _ := value.(model.Vector)

	for _, v := range data {
		mv := MetricValue{
			Metadata: make(map[string]string),
		}

		for k, v := range v.Metric {
			mv.Metadata[string(k)] = string(v)
		}

		mv.Sample = &Point{float64(v.Timestamp) / 1000, float64(v.Value)}

		res.MetricValues = append(res.MetricValues, mv)
	}

	return res
}

func GetSortedNamespaceMetrics(m *Metric) NamespaceMetricSlice {
	var res NamespaceMetricSlice

	for _, v := range m.MetricData.MetricValues {
		r := struct {
			Namespace string
			Value     float64
		}{
			Namespace: v.Metadata["namespace"],
			Value:     v.Sample[1],
		}
		if r.Value > 0 {
			res = append(res, r)
		}
	}

	sort.Sort(res)
	return res
}

func GetSortedUserMetrics(m *Metric) UserMetricSlice {
	var res UserMetricSlice
	if len(m.MetricData.MetricValues) > 1 {
		value := m.MetricData.MetricValues[0]
		r := struct {
			User  string
			Value float64
		}{
			User:  value.Metadata["user"],
			Value: value.Sample[1],
		}
		if r.Value > 0 {
			res = append(res, r)
		}
	}
	sort.Sort(res)
	return res
}

func makeUserMetricExpr(tmpl string, username string) string {
	var userSelector string
	if username != "" {
		userSelector = fmt.Sprintf(`user=~"%s"`, username)
	}
	return strings.Replace(tmpl, "$1", userSelector, -1)
}

func makeExpr(metric string, opts QueryOptions) string {
	tmpl := promQLTemplates[metric]
	switch opts.Level {
	case LevelCluster:
		return tmpl
	case LevelUser:
		return makeUserMetricExpr(tmpl, opts.UserName)
	default:
		return tmpl
	}
}

func GetCurUserResource(ctx context.Context, kubeConfig *rest.Config, username string) (*ClusterMetrics, error) {
	p, err := New(Endpoint)
	if err != nil {
		return nil, err
	}
	opts := QueryOptions{
		Level:    LevelUser,
		UserName: username,
	}
	metrics := p.GetNamedMetrics(ctx, []string{"user_cpu_usage", "user_memory_usage"}, time.Now(), opts)
	if err != nil {
		return nil, err
	}
	cpuS, err := kubesphere.GetUserCPULimit(ctx, kubeConfig, username)
	if err != nil && err.Error() != "user annotation bytetrade.io/user-cpu-limit not found" {
		return nil, err
	}
	memoryS, err := kubesphere.GetUserMemoryLimit(ctx, kubeConfig, username)
	if err != nil && err.Error() != "user annotation bytetrade.io/user-memory-limit not found" {
		return nil, err
	}
	cpuLimit, memoryLimit := float64(0), float64(0)
	if cpuS != "" {
		c, _ := resource.ParseQuantity(cpuS)
		cpuLimit = c.AsApproximateFloat64()
	}
	if memoryS != "" {
		m, _ := resource.ParseQuantity(memoryS)
		memoryLimit = m.AsApproximateFloat64()
	}
	var userMetrics ClusterMetrics
	userMetrics.CPU.Total = cpuLimit
	userMetrics.Memory.Total = memoryLimit

	for _, m := range metrics {
		switch m.MetricName {
		case "user_cpu_usage":
			userMetrics.CPU.Usage = GetValue(&m)
		case "user_memory_usage":
			userMetrics.Memory.Usage = GetValue(&m)
		}
	}
	opts = QueryOptions{
		Level: LevelCluster,
	}
	cMetrics := p.GetNamedMetrics(ctx, []string{"cluster_cpu_total", "cluster_memory_total"}, time.Now(), opts)
	for _, m := range cMetrics {
		switch m.MetricName {
		case "cluster_cpu_total":
			if userMetrics.CPU.Total == 0 {
				userMetrics.CPU.Total = GetValue(&m)
			}
		case "cluster_memory_total":
			if userMetrics.Memory.Total == 0 {
				userMetrics.Memory.Total = GetValue(&m)
			}
		}
	}

	return &userMetrics, nil
}

func GetValue(m *Metric) float64 {
	if len(m.MetricData.MetricValues) == 0 {
		return float64(0)
	}
	return m.MetricData.MetricValues[0].Sample[1]
}
