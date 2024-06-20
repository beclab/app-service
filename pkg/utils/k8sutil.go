package utils

import (
	"context"

	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CalicoTunnelAddrAnnotation annotation key for calico tunnel address.
const CalicoTunnelAddrAnnotation = "projectcalico.org/IPv4IPIPTunnelAddr"

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
