// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ApplicationManagerLister helps list ApplicationManagers.
// All objects returned here must be treated as read-only.
type ApplicationManagerLister interface {
	// List lists all ApplicationManagers in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ApplicationManager, err error)
	// Get retrieves the ApplicationManager from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ApplicationManager, error)
	ApplicationManagerListerExpansion
}

// applicationManagerLister implements the ApplicationManagerLister interface.
type applicationManagerLister struct {
	indexer cache.Indexer
}

// NewApplicationManagerLister returns a new ApplicationManagerLister.
func NewApplicationManagerLister(indexer cache.Indexer) ApplicationManagerLister {
	return &applicationManagerLister{indexer: indexer}
}

// List lists all ApplicationManagers in the indexer.
func (s *applicationManagerLister) List(selector labels.Selector) (ret []*v1alpha1.ApplicationManager, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ApplicationManager))
	})
	return ret, err
}

// Get retrieves the ApplicationManager from the index for a given name.
func (s *applicationManagerLister) Get(name string) (*v1alpha1.ApplicationManager, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("applicationmanager"), name)
	}
	return obj.(*v1alpha1.ApplicationManager), nil
}
