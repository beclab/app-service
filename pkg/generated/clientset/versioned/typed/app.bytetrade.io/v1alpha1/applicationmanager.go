// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	scheme "bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ApplicationManagersGetter has a method to return a ApplicationManagerInterface.
// A group's client should implement this interface.
type ApplicationManagersGetter interface {
	ApplicationManagers() ApplicationManagerInterface
}

// ApplicationManagerInterface has methods to work with ApplicationManager resources.
type ApplicationManagerInterface interface {
	Create(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.CreateOptions) (*v1alpha1.ApplicationManager, error)
	Update(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.UpdateOptions) (*v1alpha1.ApplicationManager, error)
	UpdateStatus(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.UpdateOptions) (*v1alpha1.ApplicationManager, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.ApplicationManager, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.ApplicationManagerList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ApplicationManager, err error)
	ApplicationManagerExpansion
}

// applicationManagers implements ApplicationManagerInterface
type applicationManagers struct {
	client rest.Interface
}

// newApplicationManagers returns a ApplicationManagers
func newApplicationManagers(c *AppV1alpha1Client) *applicationManagers {
	return &applicationManagers{
		client: c.RESTClient(),
	}
}

// Get takes name of the applicationManager, and returns the corresponding applicationManager object, and an error if there is any.
func (c *applicationManagers) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ApplicationManager, err error) {
	result = &v1alpha1.ApplicationManager{}
	err = c.client.Get().
		Resource("applicationmanagers").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ApplicationManagers that match those selectors.
func (c *applicationManagers) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ApplicationManagerList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.ApplicationManagerList{}
	err = c.client.Get().
		Resource("applicationmanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested applicationManagers.
func (c *applicationManagers) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("applicationmanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a applicationManager and creates it.  Returns the server's representation of the applicationManager, and an error, if there is any.
func (c *applicationManagers) Create(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.CreateOptions) (result *v1alpha1.ApplicationManager, err error) {
	result = &v1alpha1.ApplicationManager{}
	err = c.client.Post().
		Resource("applicationmanagers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(applicationManager).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a applicationManager and updates it. Returns the server's representation of the applicationManager, and an error, if there is any.
func (c *applicationManagers) Update(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.UpdateOptions) (result *v1alpha1.ApplicationManager, err error) {
	result = &v1alpha1.ApplicationManager{}
	err = c.client.Put().
		Resource("applicationmanagers").
		Name(applicationManager.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(applicationManager).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *applicationManagers) UpdateStatus(ctx context.Context, applicationManager *v1alpha1.ApplicationManager, opts v1.UpdateOptions) (result *v1alpha1.ApplicationManager, err error) {
	result = &v1alpha1.ApplicationManager{}
	err = c.client.Put().
		Resource("applicationmanagers").
		Name(applicationManager.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(applicationManager).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the applicationManager and deletes it. Returns an error if one occurs.
func (c *applicationManagers) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Resource("applicationmanagers").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *applicationManagers) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("applicationmanagers").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched applicationManager.
func (c *applicationManagers) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ApplicationManager, err error) {
	result = &v1alpha1.ApplicationManager{}
	err = c.client.Patch(pt).
		Resource("applicationmanagers").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
