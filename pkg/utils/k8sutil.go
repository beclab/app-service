package utils

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"

	srvconfig "github.com/containerd/containerd/services/server/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CalicoTunnelAddrAnnotation annotation key for calico tunnel address.
const CalicoTunnelAddrAnnotation = "projectcalico.org/IPv4IPIPTunnelAddr"
const DefaultRegistry = "https://registry-1.docker.io"

// GetAllNodesPodCIDRs returns all node pod's cidr.
func GetAllNodesPodCIDRs() (cidrs []string) {
	cidrs = make([]string, 0)

	config, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("Failed to get kube config err=%v", err)
		return
	}
	c, err := clientset.New(config)
	if err != nil {
		klog.Errorf("Failed to create new client err=%v", err)
		return
	}

	nodes, err := c.KubeClient.Kubernetes().CoreV1().Nodes().
		List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to list nodes err=%v", err)
		return
	}

	for _, node := range nodes.Items {
		if node.Spec.PodCIDR != "" {
			cidrs = append(cidrs, node.Spec.PodCIDR)
		}
	}
	return cidrs
}

// GetAllNodesTunnelIPCIDRs returns all node tunnel's ip cidr.
func GetAllNodesTunnelIPCIDRs() (cidrs []string) {
	cidrs = make([]string, 0)

	config, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("Failed to get kube config: %v", err)
		return
	}
	c, err := clientset.New(config)
	if err != nil {
		klog.Errorf("Failed to create new client: %v", err)
		return
	}

	nodes, err := c.KubeClient.Kubernetes().CoreV1().Nodes().
		List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Failed to list nodes err=%v", err)
		return
	}

	for _, node := range nodes.Items {
		if ip, ok := node.Annotations[CalicoTunnelAddrAnnotation]; ok {
			cidrs = append(cidrs, ip+"/32")
		}
	}

	return cidrs
}

func FindGpuTypeFromNodes(ctx context.Context, clientSet *kubernetes.Clientset) (string, error) {
	gpuType := "none"
	nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return gpuType, err
	}
	for _, n := range nodes.Items {
		if _, ok := n.Status.Capacity[constants.NvidiaGPU]; ok {
			if _, ok = n.Status.Capacity[constants.NvshareGPU]; ok {
				return "nvshare", nil

			}
			gpuType = "nvidia"
		}
		if _, ok := n.Status.Capacity[constants.VirtAiTechVGPU]; ok {
			return "virtaitech", nil
		}
	}
	return gpuType, nil
}

func IsNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func GetMirrorsEndpoint() (ep []string) {
	config := &srvconfig.Config{}
	err := srvconfig.LoadConfig("/etc/containerd/config.toml", config)
	if err != nil {
		klog.Infof("load mirrors endpoint failed err=%v", err)
		return
	}
	plugins := config.Plugins["io.containerd.grpc.v1.cri"]
	r := plugins.GetPath([]string{"registry", "mirrors", "docker.io", "endpoint"})
	if r == nil {
		return
	}
	for _, e := range r.([]interface{}) {
		ep = append(ep, e.(string))
	}
	return ep
}

// ReplacedImageRef return replaced image ref and true if mirror is support http
func ReplacedImageRef(mirrorsEndpoint []string, oldImageRef string, checkConnection bool) (string, bool) {
	if len(mirrorsEndpoint) == 0 {
		return oldImageRef, false
	}
	plainHTTP := false
	for _, ep := range mirrorsEndpoint {
		if ep != "" && ep != DefaultRegistry {
			url, err := url.Parse(ep)
			if err != nil {
				continue
			}
			if url.Scheme == "http" {
				plainHTTP = true
			}
			if checkConnection {
				host := url.Host
				if !hasPort(url.Host) {
					if url.Scheme == "https" {
						host = net.JoinHostPort(url.Host, "443")
					} else {
						host = net.JoinHostPort(url.Host, "80")
					}
				}
				conn, err := net.DialTimeout("tcp", host, 2*time.Second)
				if err != nil {
					continue
				}
				if conn != nil {
					conn.Close()
				}
			}

			parts := strings.Split(oldImageRef, "/")
			klog.Infof("parts: %s", parts)
			if parts[0] == "docker.io" {
				parts[0] = url.Host
			}
			klog.Infof("parts2: %s", parts)
			return strings.Join(parts, "/"), plainHTTP
		}
	}
	return oldImageRef, false
}

func hasPort(s string) bool { return strings.LastIndex(s, ":") > strings.LastIndex(s, "]") }
