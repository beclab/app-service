package controllers

import (
	"context"
	"fmt"
	"strings"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/security"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/go-logr/logr"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "security-controller"
)

// SecurityReconciler represents a reconciler for managing security
type SecurityReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Logger        *logr.Logger
	DynamicClient dynamic.Interface
}

var loggerKey struct{}

// SetupWithManager sets up the SecurityReconciler with the provided controller manager
func (r *SecurityReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}
	if r.Logger == nil {
		l := ctrl.Log.WithName("controllers").WithName(controllerName)
		r.Logger = &l
	}
	c, err := ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&corev1.Namespace{}).
		Build(r)

	if err != nil {
		return err
	}

	// watch the networkpolicy enqueue formarted request
	err = c.Watch(
		&source.Kind{Type: &netv1.NetworkPolicy{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name: h.GetNamespace(),
				}}}
			}))

	if err != nil {
		return err
	}

	watches := []client.Object{
		&appsv1.Deployment{},
		&appsv1.StatefulSet{},
		&corev1.Node{},
	}

	// watch the object installed by app-installer
	for _, w := range watches {
		if err = r.addWatch(ctx, c, w); err != nil {
			return err
		}
	}
	return nil
}

func (r *SecurityReconciler) addWatch(ctx context.Context, c controller.Controller, watchedObject client.Object) error {
	return c.Watch(
		&source.Kind{Type: watchedObject},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				if _, ok := h.(*corev1.Node); ok {
					r.Logger.Info("node event fired, modify network policy to add node tunnel ip")
					if reqs, err := r.namespacesShouldAllowNodeTunnel(ctx); err == nil {
						return reqs
					}
					return nil
				}

				if _, ok := h.(*corev1.Namespace); ok {
					return []reconcile.Request{{NamespacedName: types.NamespacedName{
						Name: h.GetName(),
					}}}
				}

				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name: h.GetNamespace(),
				}}}
			}),
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return isNodeChanged(e.ObjectNew, e.ObjectOld) || isApp(e.ObjectNew, e.ObjectOld) || isWorkflow(e.ObjectNew, e.ObjectOld)
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return isNodeChanged(e.Object) || isApp(e.Object) || isWorkflow(e.Object)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return isNodeChanged(e.Object) || isApp(e.Object) || isWorkflow(e.Object)
			},
		})
}

// Reconcile implements the reconciliation loop for the SecurityReconciler
func (r *SecurityReconciler) Reconcile(rootCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("namespace", req.NamespacedName)
	ctx := context.WithValue(rootCtx, loggerKey, logger)

	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, namespace); err != nil {
		logger.Error(err, "Failed to get namespace")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("namespace reconcile request")

	if namespace.ObjectMeta.DeletionTimestamp.IsZero() {
		// When a new namespace that's not a specific one (system, user internal) was created,
		// we don't give it any labels until the app installer deploys the pods.
		// non-labels namespace can't access any other namespace's network
		if err := r.reconcileNamespaceLabels(ctx, namespace); err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("Conflict while update namespace labels.")
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileNetworkPolicy(ctx, namespace); err != nil {
		if apierrors.IsConflict(err) {
			logger.Info("Conflict while update namespace network policy.")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SecurityReconciler) reconcileNamespaceLabels(ctx context.Context, ns *corev1.Namespace) error {
	logger := ctx.Value(loggerKey).(logr.Logger)
	updated := false
	if security.IsOSSystemNamespace(ns.Name) {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}

		if label, ok := ns.Labels[security.NamespaceTypeLabel]; !ok || label != security.System {
			ns.Labels[security.NamespaceTypeLabel] = security.System
			updated = true
		}
	} else if security.IsUnderLayerNamespace(ns.Name) {
		// do nothing
	} else if ok, owner := security.IsUserInternalNamespaces(ns.Name); ok {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}

		if label, ok := ns.Labels[security.NamespaceTypeLabel]; !ok || label != security.Internal {
			ns.Labels[security.NamespaceTypeLabel] = security.Internal
			updated = true
		}

		if label, ok := ns.Labels[security.NamespaceOwnerLabel]; !ok || label != owner {
			ns.Labels[security.NamespaceOwnerLabel] = owner
			updated = true
		}
	} else {
		owner, err := r.findOwnerOfNamespace(ctx, ns)
		if err != nil {
			return err
		}

		if owner != "" {
			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}

			if label, ok := ns.Labels[security.NamespaceOwnerLabel]; !ok || label != owner {
				ns.Labels[security.NamespaceOwnerLabel] = owner
				updated = true
			}
		} else {
			// remove owner label
			if _, ok := ns.Labels[security.NamespaceOwnerLabel]; ok {
				delete(ns.Labels, security.NamespaceOwnerLabel)
				updated = true
			}
		}

	}
	if updated {
		logger.Info("Update labels of namespace")
		err := r.Update(ctx, ns)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *SecurityReconciler) createOrUpdateNetworkPolicy(ctx context.Context,
	ns *corev1.Namespace,
	npName string,
	networkPolicy *netv1.NetworkPolicy,
	networkPolicyFix func(np *netv1.NetworkPolicy),
) error {
	var nps netv1.NetworkPolicyList
	key := client.ObjectKey{
		Namespace: ns.Name,
		Name:      npName,
	}
	err := r.List(ctx, &nps, client.InNamespace(ns.Name))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	found := false
	for _, np := range nps.Items {
		if np.Name == key.Name && np.Namespace == key.Namespace {
			np.Spec = *networkPolicy.Spec.DeepCopy()
			if networkPolicyFix != nil {
				networkPolicyFix(&np)
			}
			if err := r.Update(ctx, &np); err != nil {
				return err
			}
			found = true
		} else {
			if err := r.Delete(ctx, &np); err != nil {
				return err
			}
		}
	}

	if apierrors.IsNotFound(err) || !found {
		np := *networkPolicy.DeepCopy()
		np.Name = npName
		np.Namespace = ns.Name
		if networkPolicyFix != nil {
			networkPolicyFix(&np)
		}

		if err := r.Create(ctx, &np); err != nil {
			return err
		}
	}

	return nil
}

func (r *SecurityReconciler) reconcileNetworkPolicy(ctx context.Context, ns *corev1.Namespace) error {
	logger := ctx.Value(loggerKey).(logr.Logger)
	finalizer := "finalizers.bytetrade.io/namespaces"

	if ns.ObjectMeta.DeletionTimestamp.IsZero() {
		if !funk.Contains(ns.ObjectMeta.Finalizers, finalizer) {
			ns.ObjectMeta.Finalizers = append(ns.ObjectMeta.Finalizers, finalizer)
			if err := r.Update(ctx, ns); err != nil {
				return err
			}
		}

		npName := ""
		var networkPolicy *netv1.NetworkPolicy
		var npFix func(np *netv1.NetworkPolicy)
		if security.IsUnderLayerNamespace(ns.Name) {
			npName = "underlayer-system-np"
			networkPolicy = security.NPUnderLayerSystem.DeepCopy()
			npFix = nil
		} else if security.IsOSSystemNamespace(ns.Name) {
			npName = "os-system-np"
			networkPolicy = security.NPOSSystem.DeepCopy()
			npFix = func(np *netv1.NetworkPolicy) {
				np.Spec.Ingress = append(np.Spec.Ingress, netv1.NetworkPolicyIngressRule{
					From: security.NodeTunnelRule(),
				})
			}
		} else if security.IsUserSystemNamespaces(ns.Name) {
			npName = "user-system-np"
			networkPolicy = security.NPUserSystem.DeepCopy()
			npFix = func(np *netv1.NetworkPolicy) {
				owner := ns.Labels[security.NamespaceOwnerLabel]
				logger.Info("update network policy", "name", npName, "owner", owner)
				np.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels[security.NamespaceOwnerLabel] = owner
			}
		} else if security.IsUserSpaceNamespaces(ns.Name) {
			npName = "user-space-np"
			networkPolicy = security.NPUserSpace.DeepCopy()
			npFix = func(np *netv1.NetworkPolicy) {
				owner := ns.Labels[security.NamespaceOwnerLabel]
				logger.Info("update network policy", "name", npName, "owner", owner)
				np.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels[security.NamespaceOwnerLabel] = owner
				np.Spec.Ingress = append(np.Spec.Ingress, netv1.NetworkPolicyIngressRule{
					From: security.NodeTunnelRule(),
				})
			}
		} else if owner, ok := ns.Labels[security.NamespaceOwnerLabel]; ok && owner != "" {
			// app namespace networkpolicy
			npName = "app-np"
			networkPolicy = security.NPAppSpace.DeepCopy()
			npFix = func(np *netv1.NetworkPolicy) {
				logger.Info("Update network policy", "name", npName, "owner", owner)
				np.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels[security.NamespaceOwnerLabel] = owner

				// get app name from np namespace
				appName := getAppNameFromNPName(np.Namespace, owner)

				if len(appName) > 0 {
					appName = fmt.Sprintf("%s-%s", np.Namespace, appName)
					key := types.NamespacedName{Name: appName}
					var depApp v1alpha1.Application
					err := r.Get(context.Background(), key, &depApp)
					if err != nil {
						logger.Info("Get app info ", "name", appName, "err", err)
					}
					//
					if appRefs, ok := depApp.Spec.Settings["clusterAppRef"]; ok {

						for _, app := range strings.Split(appRefs, ",") {
							np.Spec.Ingress[0].From = append(np.Spec.Ingress[0].From, netv1.NetworkPolicyPeer{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										constants.ApplicationClusterDep: app,
									},
								},
							})
						}
					}
				}

			}
		} else {
			npName = "others-np"
			networkPolicy = security.NPDenyAll.DeepCopy()
			npFix = func(np *netv1.NetworkPolicy) {
				logger.Info("Update network policy", "name", npName)
			}
		}

		// add the namespace itself to the policy
		if networkPolicy.Spec.Ingress == nil {
			networkPolicy.Spec.Ingress = []netv1.NetworkPolicyIngressRule{}
		}

		if len(networkPolicy.Spec.Ingress) == 0 {
			networkPolicy.Spec.Ingress = append(networkPolicy.Spec.Ingress, netv1.NetworkPolicyIngressRule{
				From: []netv1.NetworkPolicyPeer{},
			})
		}

		if r.namespaceMustAdd(networkPolicy, ns) {
			networkPolicy.Spec.Ingress[0].From = append(networkPolicy.Spec.Ingress[0].From, netv1.NetworkPolicyPeer{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": ns.Name,
					},
				},
			},
			)
		}

		if err := r.createOrUpdateNetworkPolicy(
			ctx,
			ns,
			npName,
			networkPolicy,
			npFix,
		); err != nil {
			return err
		}

	} else {
		// delete network policy
		var networkPolicies netv1.NetworkPolicyList
		err := r.List(ctx, &networkPolicies, client.InNamespace(ns.Name))
		if err != nil {
			return err
		}

		for _, n := range networkPolicies.Items {
			if err := r.Delete(ctx, &n); err != nil {
				return err
			}
		}

		// remove finalizer
		ns.ObjectMeta.Finalizers = funk.FilterString(ns.ObjectMeta.Finalizers,
			func(item string) bool {
				return item != finalizer
			},
		)
		if err := r.Update(ctx, ns); err != nil {
			return err
		}

	}
	return nil
}

func (r *SecurityReconciler) findOwnerOfNamespace(ctx context.Context, ns *corev1.Namespace) (string, error) {
	var deployemnts appsv1.DeploymentList

	// get deployments installed by app installer
	if err := r.List(ctx, &deployemnts, client.InNamespace(ns.Name)); err == nil {
		for _, d := range deployemnts.Items {
			if d.GetLabels() == nil {
				continue
			}

			owner, ok := d.GetLabels()[constants.ApplicationOwnerLabel]
			if ok && owner != "" {
				return owner, nil
			}
		} // end loop deployment.Items
	}

	// try to get statefulset
	var statefulSets appsv1.StatefulSetList
	if err := r.List(ctx, &statefulSets, client.InNamespace(ns.Name)); err == nil {
		for _, d := range statefulSets.Items {
			if d.GetLabels() == nil {
				continue
			}

			owner, ok := d.GetLabels()[constants.ApplicationOwnerLabel]
			if ok && owner != "" {
				return owner, nil
			}
		} // end loop sts.Items
	}

	// try to get argo workflow
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "cronworkflows",
	}

	if workflows, err := r.DynamicClient.Resource(gvr).Namespace(ns.Name).List(ctx, metav1.ListOptions{}); err == nil {
		for _, w := range workflows.Items {
			if w.GetLabels() == nil {
				continue
			}

			owner, ok := w.GetLabels()[constants.WorkflowOwnerLabel]
			if ok && owner != "" {
				return owner, nil
			}
		}
	}

	return "", nil
}

func (r *SecurityReconciler) namespaceMustAdd(networkPolicy *netv1.NetworkPolicy, ns *corev1.Namespace) bool {
	for _, i := range networkPolicy.Spec.Ingress {
		for _, f := range i.From {
			if f.NamespaceSelector != nil && f.NamespaceSelector.MatchLabels != nil {
				if v, ok := f.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]; ok && v == ns.Name {
					return false
				}
			}
		}
	}

	return true
}

func (r *SecurityReconciler) namespacesShouldAllowNodeTunnel(ctx context.Context) ([]reconcile.Request, error) {
	schemeGroupVersionResource := schema.GroupVersionResource{Group: "iam.kubesphere.io", Version: "v1alpha2", Resource: "users"}
	users, err := r.DynamicClient.Resource(schemeGroupVersionResource).List(ctx, metav1.ListOptions{})
	if err != nil {
		r.Logger.Error(err, "Failed to list user")
		return nil, err
	}

	reqs := []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: "os-system",
			},
		},
	}
	for _, u := range users.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "user-space-" + u.GetName(),
			},
		})
	}

	return reqs, nil
}

func isNodeChanged(obj ...metav1.Object) bool {
	o := obj[0]
	// network policy should be reconciled when nodes are changed
	if _, ok := o.(*corev1.Node); ok {
		if len(obj) > 1 {
			o1 := obj[0].(*corev1.Node)
			o2 := obj[1].(*corev1.Node)

			return o1.Annotations[utils.CalicoTunnelAddrAnnotation] != o2.Annotations[utils.CalicoTunnelAddrAnnotation]
		}
		return true
	}

	return false
}

func getAppNameFromNPName(ns string, owner string) string {
	if !strings.HasPrefix(ns, "user-space") &&
		!strings.HasPrefix(ns, "user-system") &&
		strings.HasSuffix(ns, "-"+owner) {
		return ns[:len(ns)-len(owner)-1]
	}
	return ""
}
