// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "bytetrade.io/web3os/app-service/pkg/generated/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Applications returns a ApplicationInformer.
	Applications() ApplicationInformer
	// ApplicationManagers returns a ApplicationManagerInformer.
	ApplicationManagers() ApplicationManagerInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Applications returns a ApplicationInformer.
func (v *version) Applications() ApplicationInformer {
	return &applicationInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}

// ApplicationManagers returns a ApplicationManagerInformer.
func (v *version) ApplicationManagers() ApplicationManagerInformer {
	return &applicationManagerInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}