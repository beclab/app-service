package security

import (
	"bytetrade.io/web3os/app-service/pkg/utils"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NamespaceTypeLabel  = "bytetrade.io/ns-type"
	NamespaceOwnerLabel = "bytetrade.io/ns-owner"
	System              = "system"
	Internal            = "user-internal"
)

var (
	// NPDenyAll is a network policy template deny all ingress.
	NPDenyAll = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress:     []netv1.NetworkPolicyIngressRule{},
		},
	} // end NPDenyAll

	// NPUnderLayerSystem is a network policy template for under layer system.
	NPUnderLayerSystem = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "k8s-app",
						Operator: metav1.LabelSelectorOpNotIn,
						Values: []string{
							"kube-dns",
						},
					},
				},
			},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: Internal,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: System,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubesphere.io/namespace": "kubesphere-system",
								},
							},
						},
					},
				},
			},
		},
	} // end NPUnderLayerSystem

	// NPOSSystem is a network policy template for os-system.
	NPOSSystem = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: Internal,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubesphere.io/namespace": "kube-system",
								},
							},
						},
					},
				},
			},
		},
	} // end NPOSSystem

	// NPUserSystem is a network policy template for user-system namespace.
	NPUserSystem = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceOwnerLabel: "",
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: System,
								},
							},
						},
					},
				},
			},
		},
	} // end NPUserSystem

	// NPUserSpace is a network policy template for user space namespace.
	NPUserSpace = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceOwnerLabel: "",
									NamespaceTypeLabel:  Internal,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: System,
								},
							},
						},
					},
				},
				{
					From: []netv1.NetworkPolicyPeer{
						{
							// allow external traffic via nodeport
							IPBlock: &netv1.IPBlock{
								CIDR:   "0.0.0.0/0",
								Except: []string{"10.233.0.0/16"},
							},
						},
					},
				},
			},
		},
	} // end NPUserSpace

	// NPAppSpace is a network policy template for application.
	NPAppSpace = netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceOwnerLabel: "",
									NamespaceTypeLabel:  Internal,
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									NamespaceTypeLabel: System,
								},
							},
						},
					},
				},
			},
		},
	} // end NPAppSpace
)

// NodeTunnelRule returns node tunnel rule.
func NodeTunnelRule() []netv1.NetworkPolicyPeer {
	var rules []netv1.NetworkPolicyPeer

	for _, cidr := range utils.GetAllNodesTunnelIPCIDRs() {
		rules = append(rules, netv1.NetworkPolicyPeer{
			IPBlock: &netv1.IPBlock{
				CIDR: cidr,
			},
		})
	}

	return rules
}
