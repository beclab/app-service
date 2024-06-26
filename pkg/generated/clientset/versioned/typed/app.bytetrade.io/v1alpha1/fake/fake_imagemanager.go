// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeImageManagers implements ImageManagerInterface
type FakeImageManagers struct {
	Fake *FakeAppV1alpha1
}

var imagemanagersResource = schema.GroupVersionResource{Group: "app.bytetrade.io", Version: "v1alpha1", Resource: "imagemanagers"}

var imagemanagersKind = schema.GroupVersionKind{Group: "app.bytetrade.io", Version: "v1alpha1", Kind: "ImageManager"}

// Get takes name of the imageManager, and returns the corresponding imageManager object, and an error if there is any.
func (c *FakeImageManagers) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ImageManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(imagemanagersResource, name), &v1alpha1.ImageManager{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ImageManager), err
}

// List takes label and field selectors, and returns the list of ImageManagers that match those selectors.
func (c *FakeImageManagers) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ImageManagerList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(imagemanagersResource, imagemanagersKind, opts), &v1alpha1.ImageManagerList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ImageManagerList{ListMeta: obj.(*v1alpha1.ImageManagerList).ListMeta}
	for _, item := range obj.(*v1alpha1.ImageManagerList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested imageManagers.
func (c *FakeImageManagers) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(imagemanagersResource, opts))
}

// Create takes the representation of a imageManager and creates it.  Returns the server's representation of the imageManager, and an error, if there is any.
func (c *FakeImageManagers) Create(ctx context.Context, imageManager *v1alpha1.ImageManager, opts v1.CreateOptions) (result *v1alpha1.ImageManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(imagemanagersResource, imageManager), &v1alpha1.ImageManager{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ImageManager), err
}

// Update takes the representation of a imageManager and updates it. Returns the server's representation of the imageManager, and an error, if there is any.
func (c *FakeImageManagers) Update(ctx context.Context, imageManager *v1alpha1.ImageManager, opts v1.UpdateOptions) (result *v1alpha1.ImageManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(imagemanagersResource, imageManager), &v1alpha1.ImageManager{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ImageManager), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeImageManagers) UpdateStatus(ctx context.Context, imageManager *v1alpha1.ImageManager, opts v1.UpdateOptions) (*v1alpha1.ImageManager, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(imagemanagersResource, "status", imageManager), &v1alpha1.ImageManager{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ImageManager), err
}

// Delete takes name of the imageManager and deletes it. Returns an error if one occurs.
func (c *FakeImageManagers) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(imagemanagersResource, name, opts), &v1alpha1.ImageManager{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeImageManagers) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(imagemanagersResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ImageManagerList{})
	return err
}

// Patch applies the patch and returns the patched imageManager.
func (c *FakeImageManagers) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ImageManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(imagemanagersResource, name, pt, data, subresources...), &v1alpha1.ImageManager{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ImageManager), err
}
