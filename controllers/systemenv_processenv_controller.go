package controllers

import (
	"context"
	"fmt"
	"os"
	"strings"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SystemEnvProcessEnvController only handles syncing SystemEnv values into the
// current process environment, supporting legacy aliases for compatibility.
type SystemEnvProcessEnvController struct {
	client.Client
	Scheme *runtime.Scheme
}

// legacyEnvAliases maintains backward-compatible aliases for system environment variables
// during the migration period. Keys are new env names, values are a single legacy name
// that should mirror the same value in the process environment.
var legacyEnvAliases = map[string]string{
	"OLARES_SYSTEM_DID_SERVICE":         "DID_GATE_URL",
	"OLARES_SYSTEM_CLOUD_SERVICE":       "OLARES_SYSTEM_SPACE_URL",
	"OLARES_SYSTEM_PUSH_SERVICE":        "FIREBASE_PUSH_URL",
	"OLARES_SYSTEM_FRP_INDEX_SERVICE":   "FRP_LIST_URL",
	"OLARES_SYSTEM_VPN_CONTROL_SERVICE": "TAILSCALE_CONTROLPLANE_URL",
	"OLARES_SYSTEM_MARKET_SERVICE":      "MARKET_PROVIDER",
	"OLARES_SYSTEM_CERT_SERVICE":        "TERMINUS_CERT_SERVICE_API",
	"OLARES_SYSTEM_DNS_SERVICE":         "TERMINUS_DNS_SERVICE_API",
	"OLARES_SYSTEM_CDN_SERVICE":         "DOWNLOAD_CDN_URL",
	"OLARES_SYSTEM_ROOT_PATH":           "OLARES_ROOT_DIR",
	"OLARES_SYSTEM_ROOTFS_TYPE":         "OLARES_FS_TYPE",
	"OLARES_SYSTEM_CUDA_VERSION":        "CUDA_VERSION",
	"OLARES_SYSTEM_CLUSTER_DNS_SERVICE": "COREDNS_SVC",
}

const migrationAnnotationKey = "sys.bytetrade.io/systemenv-migrated"

//+kubebuilder:rbac:groups=sys.bytetrade.io,resources=systemenvs,verbs=get;list;watch

func (r *SystemEnvProcessEnvController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("systemenv-processenv").
		For(&sysv1alpha1.SystemEnv{}).
		Complete(r)
}

func (r *SystemEnvProcessEnvController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("Reconciling SystemEnv for process env: %s", req.NamespacedName)

	var systemEnv sysv1alpha1.SystemEnv
	if err := r.Get(ctx, req.NamespacedName, &systemEnv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	value := systemEnv.GetEffectiveValue()
	if err := setEnvAndAlias(systemEnv.EnvName, value); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setEnvAndAlias sets the given env name and all of its legacy aliases
// in the current process environment. Returns an error if any setenv fails.
func setEnvAndAlias(envName, value string) error {
	if value == "" {
		klog.V(4).Infof("Skip setting env %s: empty effective value", envName)
		return nil
	}
	if err := os.Setenv(envName, value); err != nil {
		return fmt.Errorf("setenv %s failed: %w", envName, err)
	}
	klog.V(4).Infof("Set env %s", envName)
	if alias, ok := legacyEnvAliases[envName]; ok && alias != "" {
		if err := os.Setenv(alias, value); err != nil {
			return fmt.Errorf("setenv legacy alias %s for %s failed: %w", alias, envName, err)
		}
		klog.V(4).Infof("Set legacy env %s (alias of %s)", alias, envName)
	}
	return nil
}

func InitializeSystemEnvProcessEnv(ctx context.Context, c client.Client) error {
	var list sysv1alpha1.SystemEnvList
	if err := c.List(ctx, &list); err != nil {
		return fmt.Errorf("failed to list SystemEnvs: %v", err)
	}

	var errs []error
	for i := range list.Items {
		se := &list.Items[i]

		migrated := se.Annotations != nil && se.Annotations[migrationAnnotationKey] == "true"
		if !migrated {
			if alias, ok := legacyEnvAliases[se.EnvName]; ok && alias != "" {
				if legacyVal, ok := os.LookupEnv(alias); ok && legacyVal != "" {
					if se.Type == "url" && !strings.HasPrefix(legacyVal, "http") {
						legacyVal = "https://" + legacyVal
					}
					if err := utils.CheckEnvValueByType(legacyVal, se.Type); err != nil {
						klog.Warningf("Skip migrating SystemEnv %s: legacy alias %s value invalid for type %s: %v", se.EnvName, alias, se.Type, err)
					} else if se.Default != legacyVal {
						original := se.DeepCopy()
						se.Default = legacyVal
						if se.Annotations == nil {
							se.Annotations = make(map[string]string)
						}
						se.Annotations[migrationAnnotationKey] = "true"
						if err := c.Patch(ctx, se, client.MergeFrom(original)); err != nil {
							errs = append(errs, fmt.Errorf("patch SystemEnv %s default from legacy alias failed: %w", se.EnvName, err))
						}
					}
				}
			}
		}

		if err := setEnvAndAlias(se.EnvName, se.GetEffectiveValue()); err != nil {
			errs = append(errs, fmt.Errorf("set process env for %s failed: %w", se.EnvName, err))
		}
	}
	return utilerrors.NewAggregate(errs)
}
