/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	cloudprivateipconfigv1 "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeCloudPrivateIPConfigs implements CloudPrivateIPConfigInterface
type FakeCloudPrivateIPConfigs struct {
	Fake *FakeNetworkV1
}

var cloudprivateipconfigsResource = schema.GroupVersionResource{Group: "network.openshift.io", Version: "v1", Resource: "cloudprivateipconfigs"}

var cloudprivateipconfigsKind = schema.GroupVersionKind{Group: "network.openshift.io", Version: "v1", Kind: "CloudPrivateIPConfig"}

// Get takes name of the cloudPrivateIPConfig, and returns the corresponding cloudPrivateIPConfig object, and an error if there is any.
func (c *FakeCloudPrivateIPConfigs) Get(ctx context.Context, name string, options v1.GetOptions) (result *cloudprivateipconfigv1.CloudPrivateIPConfig, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(cloudprivateipconfigsResource, name), &cloudprivateipconfigv1.CloudPrivateIPConfig{})
	if obj == nil {
		return nil, err
	}
	return obj.(*cloudprivateipconfigv1.CloudPrivateIPConfig), err
}

// List takes label and field selectors, and returns the list of CloudPrivateIPConfigs that match those selectors.
func (c *FakeCloudPrivateIPConfigs) List(ctx context.Context, opts v1.ListOptions) (result *cloudprivateipconfigv1.CloudPrivateIPConfigList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(cloudprivateipconfigsResource, cloudprivateipconfigsKind, opts), &cloudprivateipconfigv1.CloudPrivateIPConfigList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &cloudprivateipconfigv1.CloudPrivateIPConfigList{ListMeta: obj.(*cloudprivateipconfigv1.CloudPrivateIPConfigList).ListMeta}
	for _, item := range obj.(*cloudprivateipconfigv1.CloudPrivateIPConfigList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested cloudPrivateIPConfigs.
func (c *FakeCloudPrivateIPConfigs) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(cloudprivateipconfigsResource, opts))
}

// Create takes the representation of a cloudPrivateIPConfig and creates it.  Returns the server's representation of the cloudPrivateIPConfig, and an error, if there is any.
func (c *FakeCloudPrivateIPConfigs) Create(ctx context.Context, cloudPrivateIPConfig *cloudprivateipconfigv1.CloudPrivateIPConfig, opts v1.CreateOptions) (result *cloudprivateipconfigv1.CloudPrivateIPConfig, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(cloudprivateipconfigsResource, cloudPrivateIPConfig), &cloudprivateipconfigv1.CloudPrivateIPConfig{})
	if obj == nil {
		return nil, err
	}
	return obj.(*cloudprivateipconfigv1.CloudPrivateIPConfig), err
}

// Update takes the representation of a cloudPrivateIPConfig and updates it. Returns the server's representation of the cloudPrivateIPConfig, and an error, if there is any.
func (c *FakeCloudPrivateIPConfigs) Update(ctx context.Context, cloudPrivateIPConfig *cloudprivateipconfigv1.CloudPrivateIPConfig, opts v1.UpdateOptions) (result *cloudprivateipconfigv1.CloudPrivateIPConfig, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(cloudprivateipconfigsResource, cloudPrivateIPConfig), &cloudprivateipconfigv1.CloudPrivateIPConfig{})
	if obj == nil {
		return nil, err
	}
	return obj.(*cloudprivateipconfigv1.CloudPrivateIPConfig), err
}

// Delete takes name of the cloudPrivateIPConfig and deletes it. Returns an error if one occurs.
func (c *FakeCloudPrivateIPConfigs) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(cloudprivateipconfigsResource, name), &cloudprivateipconfigv1.CloudPrivateIPConfig{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeCloudPrivateIPConfigs) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(cloudprivateipconfigsResource, listOpts)

	_, err := c.Fake.Invokes(action, &cloudprivateipconfigv1.CloudPrivateIPConfigList{})
	return err
}

// Patch applies the patch and returns the patched cloudPrivateIPConfig.
func (c *FakeCloudPrivateIPConfigs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *cloudprivateipconfigv1.CloudPrivateIPConfig, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(cloudprivateipconfigsResource, name, pt, data, subresources...), &cloudprivateipconfigv1.CloudPrivateIPConfig{})
	if obj == nil {
		return nil, err
	}
	return obj.(*cloudprivateipconfigv1.CloudPrivateIPConfig), err
}
