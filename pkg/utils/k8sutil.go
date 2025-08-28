package utils

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/users"
	iamv1alpha2 "github.com/beclab/api/iam/v1alpha2"

	srvconfig "github.com/containerd/containerd/services/server/config"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func FindGpuTypeFromNodes(nodes *corev1.NodeList) (string, error) {
	gpuType := "none"
	if nodes == nil {
		return gpuType, errors.New("empty node list")
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

func FindOwnerUser(ctrlClient client.Client, user *iamv1alpha2.User) (*iamv1alpha2.User, error) {
	creator := user.Annotations[users.AnnotationUserCreator]
	if creator != "cli" {
		var creatorUser iamv1alpha2.User
		err := ctrlClient.Get(context.TODO(), types.NamespacedName{Name: creator}, &creatorUser)
		if err != nil {
			return nil, err
		}
		return &creatorUser, nil
	}

	var userList iamv1alpha2.UserList
	err := ctrlClient.List(context.TODO(), &userList)
	if err != nil {
		klog.Errorf("failed to list user %v", err)
		return nil, err
	}
	for _, u := range userList.Items {
		if u.Annotations[users.UserAnnotationOwnerRole] == "owner" {
			return &u, nil
		}
	}
	return nil, errors.New("user with owner role not found")
}

func GetDeploymentName(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}

	replicaSetHash := pod.Labels["pod-template-hash"]
	if replicaSetHash == "" {
		return ""
	}

	replicaSetSuffix := fmt.Sprintf("-%s", replicaSetHash)
	return strings.Split(pod.GenerateName, replicaSetSuffix)[0] // pod.name not exists
}

var serviceAccountToken string

func GetServerServiceAccountToken() (string, error) {
	if serviceAccountToken != "" {
		return serviceAccountToken, nil
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("Failed to get config: %v", err)
		return "", err
	}

	serviceAccountToken = config.BearerToken

	return serviceAccountToken, nil
}

func GetUserServiceAccountToken(ctx context.Context, client kubernetes.Interface, user string) (string, error) {
	namespace := fmt.Sprintf("user-system-%s", user)
	tr := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{"https://kubernetes.default.svc.cluster.local"},
			ExpirationSeconds: ptr.To[int64](86400), // one day
		},
	}

	token, err := client.CoreV1().ServiceAccounts(namespace).
		CreateToken(ctx, "user-backend", tr, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Failed to create token for user %s in namespace %s: %v", user, namespace, err)
		return "", err
	}

	return token.Status.Token, nil
}
