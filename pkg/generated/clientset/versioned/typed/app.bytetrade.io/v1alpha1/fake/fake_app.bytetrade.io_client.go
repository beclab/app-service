// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned/typed/app.bytetrade.io/v1alpha1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeAppV1alpha1 struct {
	*testing.Fake
}

func (c *FakeAppV1alpha1) AppImages() v1alpha1.AppImageInterface {
	return &FakeAppImages{c}
}

func (c *FakeAppV1alpha1) Applications() v1alpha1.ApplicationInterface {
	return &FakeApplications{c}
}

func (c *FakeAppV1alpha1) ApplicationManagers() v1alpha1.ApplicationManagerInterface {
	return &FakeApplicationManagers{c}
}

func (c *FakeAppV1alpha1) ImageManagers() v1alpha1.ImageManagerInterface {
	return &FakeImageManagers{c}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeAppV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
