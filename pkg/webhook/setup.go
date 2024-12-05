package webhook

import (
	"context"
	"io/ioutil"
	"strconv"
	"strings"

	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/security"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	sandboxWebhookName                    = "sandbox-webhook"
	mutatingWebhookName                   = "sandbox-inject-webhook.bytetrade.io"
	appNsWebhookName                      = "appns-validating-webhook"
	providerRegistryWebhookName           = "provider-validating-webhook"
	providerRegistryValidatingWebhookName = "provider-registry-validating-webhook.bytetrade.io"
	validatingWebhookName                 = "appns-validating-webhook.bytetrade.io"
	gpuLimitWebhookName                   = "gpu-limit-webhook"
	mutatingWebhookGpuLimitName           = "gpu-limit-inject-webhook.bytetrade.io"
	webhookServiceName                    = "app-service"
	webhookServiceNamespace               = "os-system"
	defaultCaPath                         = "/etc/certs/ca.crt"
	evictionWebhookName                   = "kubelet-eviction-webhook"
	evictionValidatingWebhookName         = "kubelet-eviction-webhook.bytetrade.io"
)

// CreateOrUpdateSandboxMutatingWebhook creates or updates the sandbox mutating webhook.
func (wh *Webhook) CreateOrUpdateSandboxMutatingWebhook() error {
	webhookPath := "/app-service/v1/sandbox/inject"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Fail
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(30)

	mwhcLabels := map[string]string{"velero.io/exclude-from-backup": "true"}

	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}

	mwhc := admissionregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   sandboxWebhookName,
			Labels: mwhcLabels,
		},
		Webhooks: []admissionregv1.MutatingWebhook{
			{
				Name: mutatingWebhookName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.UnderLayerNamespaces,
						},
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.OSSystemNamespaces,
						},
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.GPUSystemNamespaces,
						},
					},
				},
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{admissionregv1.Create},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"}}},
	}

	if _, err := wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.Background(), &mwhc, metav1.CreateOptions{}); err != nil {
		// Webhook already exists, update the webhook in this scenario
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), mwhc.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get MutatingWebhookConfiguration name=%s err=%v", mwhc.Name, err)
				return err
			}

			mwhc.ObjectMeta.ResourceVersion = existing.ObjectMeta.ResourceVersion
			if _, err = wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.Background(), &mwhc, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update MutatingWebhookConfiguration name=%s err=%v", mwhc.Name, err)
					return err
				}
			}
		} else {
			// Webhook doesn't exist and could not be created, an error is logged and returned
			klog.Errorf("Failed to create MutatingWebhookConfiguration name=%s err=%v", mwhc.Name, err)
			return err
		}
	}

	klog.Info("Finished creating MutatingWebhookConfiguration")
	return nil
}

// CreateOrUpdateAppNamespaceValidatingWebhook creates or updates app namespace validating webhook.
func (wh *Webhook) CreateOrUpdateAppNamespaceValidatingWebhook() error {
	webhookPath := "/app-service/v1/appns/validate"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Fail
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(30)
	mwcLabels := map[string]string{}

	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}
	mwc := admissionregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   appNsWebhookName,
			Labels: mwcLabels,
		},
		Webhooks: []admissionregv1.ValidatingWebhook{
			{
				Name: validatingWebhookName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{admissionregv1.Create},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments", "statefulsets", "daemonsets"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"}}},
	}
	if _, err = wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
		Create(context.Background(), &mwc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Get(context.Background(), mwc.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get ValidatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
				return err
			}
			mwc.ObjectMeta = existing.ObjectMeta
			if _, err := wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Update(context.Background(), &mwc, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update ValidatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
					return err
				}

			}
		} else {
			klog.Errorf("Failed to create ValidatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
			return err
		}
	}
	klog.Info("Finished creating ValidatingWebhookConfiguration")

	return nil
}

// CreateOrUpdateGpuLimitMutatingWebhook creates or updates gpu limit mutating webhook.
func (wh *Webhook) CreateOrUpdateGpuLimitMutatingWebhook() error {
	webhookPath := "/app-service/v1/gpulimit/inject"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Fail
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(30)

	mwhLabels := map[string]string{"velero.io/exclude-from-backup": "true"}
	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}
	mwh := admissionregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   gpuLimitWebhookName,
			Labels: mwhLabels,
		},
		Webhooks: []admissionregv1.MutatingWebhook{
			{
				Name: mutatingWebhookGpuLimitName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				ObjectSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "tier",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"app-service"},
						},
					},
				},
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{admissionregv1.Create, admissionregv1.Update},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments", "statefulsets"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"}}},
	}
	if _, err = wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(context.Background(), &mwh, metav1.CreateOptions{}); err != nil {
		// Webhook already exists, update the webhook in this scenario
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), mwh.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get MutatingWebhookConfiguration name=%s err=%v", mwh.Name, err)
				return err
			}
			mwh.ObjectMeta.ResourceVersion = existing.ObjectMeta.ResourceVersion
			if _, err = wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.Background(), &mwh, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update MutatingWebhookConfiguration name=%s err=%v", mwh.Name, err)
					return err
				}
			}
		} else {
			klog.Errorf("Failed to create MutatingWebhookConfiguration name=%s err=%v", mwh.Name, err)
			return err
		}
	}
	klog.Infof("Finished creating MutatingWebhookConfiguration %s", gpuLimitWebhookName)
	return nil
}

func (wh *Webhook) CreateOrUpdateProviderRegistryValidatingWebhook() error {
	webhookPath := "/app-service/v1/provider-registry/validate"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Fail
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(30)
	vwcLabels := map[string]string{}

	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}
	vwc := admissionregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   providerRegistryWebhookName,
			Labels: vwcLabels,
		},
		Webhooks: []admissionregv1.ValidatingWebhook{
			{
				Name: providerRegistryValidatingWebhookName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{
							admissionregv1.Create, admissionregv1.Update,
						},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"sys.bytetrade.io"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{"providerregistries"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
	if _, err = wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
		Create(context.TODO(), &vwc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Get(context.TODO(), vwc.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
				return err
			}
			vwc.ObjectMeta = existing.ObjectMeta
			if _, err = wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Update(context.TODO(), &vwc, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
					return err
				}
			}
		} else {
			klog.Errorf("Failed to create ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
			return err
		}
	}
	klog.Info("Finished creating ValidatingWebhookConfiguration name=%s", providerRegistryWebhookName)
	return nil
}

func (wh *Webhook) CreateOrUpdateKubeletEvictionValidatingWebhook() error {
	webhookPath := "/app-service/v1/pods/kubelet/eviction"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Ignore
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(5)

	vwhcLabels := map[string]string{"velero.io/exclude-from-backup": "true"}

	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}
	vwc := admissionregv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   evictionWebhookName,
			Labels: vwhcLabels,
		},
		Webhooks: []admissionregv1.ValidatingWebhook{
			{
				Name: evictionValidatingWebhookName,
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy: &failurePolicy,
				MatchPolicy:   &matchPolicy,
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.UnderLayerNamespaces,
						},
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.OSSystemNamespaces,
						},
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   security.GPUSystemNamespaces,
						},
					},
				},
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{admissionregv1.Update},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Resources:   []string{"pods/status"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
	if _, err = wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
		Create(context.TODO(), &vwc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Get(context.Background(), vwc.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
				return err
			}
			vwc.ObjectMeta = existing.ObjectMeta
			if _, err = wh.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().
				Update(context.TODO(), &vwc, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
					return err
				}
			}
		} else {
			klog.Errorf("Failed to create ValidatingWebhookConfiguration name=%s err=%v", vwc.Name, err)
			return err
		}
	}
	klog.Info("Finished creating ValidatingWebhookConfiguration name=%s", vwc.Name)
	return nil
}

func (wh *Webhook) CreateOrUpdateCronWorkflowMutatingWebhook() error {
	webhookPath := "/app-service/v1/workflow/inject"
	port, err := strconv.Atoi(strings.Split(constants.WebhookServerListenAddress, ":")[1])
	if err != nil {
		return err
	}
	webhookPort := int32(port)
	failurePolicy := admissionregv1.Ignore
	matchPolicy := admissionregv1.Exact
	webhookTimeout := int32(5)

	mwhcLabels := map[string]string{"velero.io/exclude-from-backup": "true"}

	caBundle, err := ioutil.ReadFile(defaultCaPath)
	if err != nil {
		return err
	}
	mwc := admissionregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cron-workflow-webhook",
			Labels: mwhcLabels,
		},
		Webhooks: []admissionregv1.MutatingWebhook{
			{
				Name: "cron-workflow-webhook.bytetrade.io",
				ClientConfig: admissionregv1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregv1.ServiceReference{
						Namespace: webhookServiceNamespace,
						Name:      webhookServiceName,
						Path:      &webhookPath,
						Port:      &webhookPort,
					},
				},
				FailurePolicy:     &failurePolicy,
				MatchPolicy:       &matchPolicy,
				NamespaceSelector: &metav1.LabelSelector{},
				Rules: []admissionregv1.RuleWithOperations{
					{
						Operations: []admissionregv1.OperationType{admissionregv1.Create, admissionregv1.Update},
						Rule: admissionregv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{"cronworkflows"},
						},
					},
				},
				SideEffects: func() *admissionregv1.SideEffectClass {
					sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
					return &sideEffect
				}(),
				TimeoutSeconds:          &webhookTimeout,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
	if _, err = wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().
		Create(context.TODO(), &mwc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, err := wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().
				Get(context.Background(), mwc.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get MutatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
				return err
			}
			mwc.ObjectMeta = existing.ObjectMeta
			if _, err = wh.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().
				Update(context.TODO(), &mwc, metav1.UpdateOptions{}); err != nil {
				if !apierrors.IsConflict(err) {
					klog.Errorf("Failed to update MutatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
					return err
				}
			}
		} else {
			klog.Errorf("Failed to create MutatingWebhookConfiguration name=%s err=%v", mwc.Name, err)
			return err
		}
	}
	klog.Info("Finished creating MutatingWebhookConfiguration name=%s", mwc.Name)
	return nil
}
