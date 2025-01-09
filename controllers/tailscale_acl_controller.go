package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const tailScaleACLPolicyMd5Key = "tailscale-acl-md5"

var defaultHTTPSACL = v1alpha1.ACL{
	Action: "accept",
	Src:    []string{"*"},
	Proto:  "",
	Dst:    []string{"*:443"},
}

type ACLPolicy struct {
	ACLs          []v1alpha1.ACL `json:"acls"`
	AutoApprovers AutoApprovers  `json:"autoApprovers"`
}

type AutoApprovers struct {
	Routes   map[string][]string `json:"routes"`
	ExitNode []string            `json:"exitNode"`
}

type TailScaleACLController struct {
	client.Client
}

func (r *TailScaleACLController) SetUpWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("app's tailscale acls manager controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}
	err = c.Watch(
		&source.Kind{Type: &v1alpha1.Application{}},
		handler.EnqueueRequestsFromMapFunc(
			func(obj client.Object) []reconcile.Request {
				app, ok := obj.(*v1alpha1.Application)
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      app.Name,
					Namespace: app.Spec.Owner,
				}}}
			}),
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return true
			},
		},
	)
	if err != nil {
		return err
	}
	return nil
}

func (r *TailScaleACLController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	klog.Infof("reconcile tailscale acls request name=%v, owner=%v", req.Name, req.Namespace)

	// for this request req.Namespace is owner
	// list all apps by owner and generate acls by owner
	var apps v1alpha1.ApplicationList
	err := r.List(ctx, &apps)
	if err != nil {
		return ctrl.Result{}, err
	}
	filteredApps := make([]v1alpha1.Application, 0)
	for _, app := range apps.Items {
		if app.Spec.Owner != req.Namespace {
			continue
		}
		filteredApps = append(filteredApps, app)
	}

	tailScaleACLConfig := "tailscale-acl"
	headScaleNamespace := fmt.Sprintf("user-space-%s", req.Namespace)

	// calculate acls
	acls := make([]v1alpha1.ACL, 0)
	for _, app := range filteredApps {
		acls = append(acls, app.Spec.TailScaleACLs...)
	}
	aclPolicyByte, err := makeACLPolicy(acls)
	if err != nil {
		return ctrl.Result{}, err
	}
	klog.Infof("aclPolicyByte:string: %s", string(aclPolicyByte))
	configMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: tailScaleACLConfig, Namespace: headScaleNamespace}, configMap)
	if err != nil {
		return ctrl.Result{}, err
	}
	oldTailScaleACLPolicyMd5Sum := ""
	if configMap.Annotations != nil {
		oldTailScaleACLPolicyMd5Sum = configMap.Annotations[tailScaleACLPolicyMd5Key]
	}
	curTailScaleACLPolicyMd5Sum := utils.Md5String(string(aclPolicyByte))

	if curTailScaleACLPolicyMd5Sum != oldTailScaleACLPolicyMd5Sum {
		if configMap.Annotations == nil {
			configMap.Annotations = make(map[string]string)
		}
		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}

		configMap.Annotations[tailScaleACLPolicyMd5Key] = curTailScaleACLPolicyMd5Sum
		configMap.Data["acl.json"] = string(aclPolicyByte)
		err = r.Update(ctx, configMap)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Namespace: headScaleNamespace, Name: "headscale"}, deploy)
	if err != nil {
		return ctrl.Result{}, err
	}
	headScaleACLMd5 := ""
	if deploy.Spec.Template.Annotations != nil {
		headScaleACLMd5 = deploy.Spec.Template.Annotations[tailScaleACLPolicyMd5Key]
	}
	if headScaleACLMd5 != curTailScaleACLPolicyMd5Sum {
		if deploy.Spec.Template.Annotations == nil {
			deploy.Spec.Template.Annotations = make(map[string]string)
		}

		// update headscale deploy template annotations for rolling update
		deploy.Spec.Template.Annotations[tailScaleACLPolicyMd5Key] = curTailScaleACLPolicyMd5Sum
		err = r.Update(ctx, deploy)
		if err != nil {
			return ctrl.Result{}, err
		}
		klog.Infof("rolling update headscale...")
	}

	return ctrl.Result{}, nil
}

func makeACLPolicy(acls []v1alpha1.ACL) ([]byte, error) {
	acls = append(acls, defaultHTTPSACL)
	for i := range acls {
		acls[i].Action = "accept"
		acls[i].Src = []string{"*"}
	}
	aclPolicy := ACLPolicy{
		ACLs: acls,
		AutoApprovers: AutoApprovers{
			Routes: map[string][]string{
				"10.0.0.0/8":     {"default"},
				"172.16.0.0/12":  {"default"},
				"192.168.0.0/16": {"default"},
			},
			ExitNode: []string{},
		},
	}
	aclPolicyByte, err := json.Marshal(aclPolicy)
	if err != nil {
		return nil, err
	}
	return aclPolicyByte, nil
}