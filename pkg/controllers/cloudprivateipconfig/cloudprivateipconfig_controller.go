package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"

	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controllers"
	cloudprivateipconfig "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1"
	cloudprivateipconfigclientset "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/clientset/versioned"
	cloudprivateipconfigscheme "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/clientset/versioned/scheme"
	cloudprivateipconfiginformers "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/informers/externalversions/cloudprivateipconfig/v1"
	cloudprivateipconfiglisters "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/listers/cloudprivateipconfig/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
)

// CloudPrivateIPConfigController is the controller implementation for CloudPrivateIPConfig resources
type CloudPrivateIPConfigController struct {
	// "Extends" our generic controller
	controller.CloudNetworkConfigController
	// Implements its own Node lister
	nodeInformer corelisters.NodeLister
	// Implements its own lister and clientset for its own API group
	cloudPrivateIPConfigClientset cloudprivateipconfigclientset.Interface
	cloudPrivateIPConfigLister    cloudprivateipconfiglisters.CloudPrivateIPConfigLister
}

// NewCloudPrivateIPConfigController returns a new CloudPrivateIPConfig controller
func NewCloudPrivateIPConfigController(
	kubeclientset kubernetes.Interface,
	cloudProviderClient cloudprovider.CloudProviderIntf,
	cloudPrivateIPConfigClientset cloudprivateipconfigclientset.Interface,
	cloudPrivateIPConfigInformer cloudprivateipconfiginformers.CloudPrivateIPConfigInformer,
	nodeInformer coreinformers.NodeInformer) *CloudPrivateIPConfigController {

	utilruntime.Must(cloudprivateipconfigscheme.AddToScheme(scheme.Scheme))

	cloudNetworkConfigController := controller.NewCloudNetworkConfigController(
		kubeclientset,
		cloudProviderClient,
		controller.CloudPrivateIPConfigControllerAgentName,
		[]cache.InformerSynced{cloudPrivateIPConfigInformer.Informer().HasSynced, nodeInformer.Informer().HasSynced},
	)

	controller := &CloudPrivateIPConfigController{
		CloudNetworkConfigController:  cloudNetworkConfigController,
		nodeInformer:                  nodeInformer.Lister(),
		cloudPrivateIPConfigClientset: cloudPrivateIPConfigClientset,
		cloudPrivateIPConfigLister:    cloudPrivateIPConfigInformer.Lister(),
	}

	cloudPrivateIPConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCloudPrivateIPConfig,
		UpdateFunc: func(old, new interface{}) {
			// Only enqueue on spec change
			oldCloudPrivateIPConfig, _ := old.(cloudprivateipconfig.CloudPrivateIPConfig)
			newCloudPrivateIPConfig, _ := new.(cloudprivateipconfig.CloudPrivateIPConfig)
			if !reflect.DeepEqual(oldCloudPrivateIPConfig.Spec, newCloudPrivateIPConfig.Spec) {
				controller.enqueueCloudPrivateIPConfig(new)
			}
		},
		DeleteFunc: controller.dequeueCloudPrivateIPConfig,
	})
	return controller
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the CloudPrivateIPConfig
// resource with the current status of the resource. We only end up here in three
// situations:
//  - ADD
//  - UPDATE where the .spec has changed
//  - Incorrect or incomplete last sync, we process the retry in this term
// so always compute a "directional" diff between .spec and .status and assign/remove
// the diff in the direction we need.
func (c *CloudPrivateIPConfigController) syncHandler(key string) error {
	// Convert the key into a distinct name (since CloudPrivateIPConfig is cluster-scoped)
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	cloudPrivateIPConfig, err := c.cloudPrivateIPConfigLister.Get(name)
	if err != nil {
		// The CloudPrivateIPConfig resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("CloudPrivateIPConfig '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}
	toAssign, toRelease := c.computeRequestDiff(cloudPrivateIPConfig)
	for _, privateIPRelease := range toRelease {
		// We only delete whetever has been previously assigned
		// so no need to validate the IP address here
		ip := net.ParseIP(privateIPRelease.IP)
		// If we can't delete stale stuff, then stop right now
		// and retry later before starting to assign new stuff
		node, err := c.nodeInformer.Get(privateIPRelease.Node)
		if err != nil {
			return err
		}
		if err := c.CloudProviderClient.ReleasePrivateIP(ip, node); err != nil {
			return err
		}
	}
	newStatus := cloudprivateipconfig.CloudPrivateIPConfigStatus{}
	for _, privateIPAssign := range toAssign {
		// No need to validate the IP address here
		// all enqueued items will have a valid spec
		ip := net.ParseIP(privateIPAssign.IP)
		node, err := c.nodeInformer.Get(privateIPAssign.Node)
		if err != nil {
			return err
		}
		if err := c.CloudProviderClient.AssignPrivateIP(ip, node); err == nil {
			newStatus.Items = append(newStatus.Items, privateIPAssign)
		}
	}
	if len(newStatus.Items) == len(toAssign) {
		return c.updateCloudPrivateIPConfigControllerStatus(cloudPrivateIPConfig, newStatus)
	}
	c.updateCloudPrivateIPConfigControllerStatus(cloudPrivateIPConfig, newStatus)
	return fmt.Errorf("CloudPrivateIPConfig: '%s' has been updated with an incomplete status, will retry during next sync", key)
}

func (c *CloudPrivateIPConfigController) updateCloudPrivateIPConfigControllerStatus(cloudPrivateIPConfig *cloudprivateipconfig.CloudPrivateIPConfig, newStatus cloudprivateipconfig.CloudPrivateIPConfigStatus) error {
	cloudPrivateIPConfigCopy := cloudPrivateIPConfig.DeepCopy()
	cloudPrivateIPConfigCopy.Status = newStatus
	_, err := c.cloudPrivateIPConfigClientset.NetworkV1().CloudPrivateIPConfigs().Update(context.TODO(), cloudPrivateIPConfigCopy, metav1.UpdateOptions{})
	return err
}

// enqueueCloudPrivateIPConfig takes a CloudPrivateIPConfig resource and converts
// it into a name string which is then put onto the work queue. This method should
// *not* be passed resources of any type other than CloudPrivateIPConfig. This
// method also validates all imposed requirements on the CloudPrivateIPConfig stanza
// (mainly: unique set of defined cloudprivateipconfig.CloudPrivateIPConfigItem in
// .spec)
func (c *CloudPrivateIPConfigController) enqueueCloudPrivateIPConfig(obj interface{}) {
	var key string
	var err error
	cloudPrivateIPConfig, isObjectTypeCorrect := obj.(cloudprivateipconfig.CloudPrivateIPConfig)
	if !isObjectTypeCorrect {
		utilruntime.HandleError(fmt.Errorf("spurious object detected, not of type: CloudPrivateIPConfig"))
		return
	}
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	if err := c.isValidSpec(cloudPrivateIPConfig.Spec.Items); err != nil {
		ref := corev1.ObjectReference{
			Kind: cloudPrivateIPConfig.GetObjectKind().GroupVersionKind().Kind,
			Name: cloudPrivateIPConfig.Name,
		}
		c.Recorder.Eventf(&ref, corev1.EventTypeWarning, "InvalidObject", "CloudPrivateIPConfig: '%s' has an invalid .spec stanza, err: %v", cloudPrivateIPConfig.Name, err)
		utilruntime.HandleError(fmt.Errorf("CloudPrivateIPConfig: '%s' has an invalid .spec stanza, err: %v", key, err))
		return
	}
	c.Workqueue.Add(key)
}

// dequeueCloudPrivateIPConfig takes a CloudPrivateIPConfig resource and converts it into a name
// string. It proceeds to releasing any privdate IP assignment made and de-queuing the item from
// the workqueue. This method should *not* be passed resources of any type other than CloudPrivateIPConfig.
func (c *CloudPrivateIPConfigController) dequeueCloudPrivateIPConfig(obj interface{}) {
	var key string
	var err error
	cloudPrivateIPConfig, isObjectTypeCorrect := obj.(cloudprivateipconfig.CloudPrivateIPConfig)
	if !isObjectTypeCorrect {
		utilruntime.HandleError(fmt.Errorf("spurious object detected, not of type: CloudPrivateIPConfig"))
		return
	}
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	cloudPrivateIPConfig.Status = c.releaseStatusAssignment(cloudPrivateIPConfig)
	// Mark the object as done only if we've been able to cleanup, otherwise
	// retry a couple of times and try to get it to 0, then give up
	if len(cloudPrivateIPConfig.Status.Items) == 0 {
		c.Workqueue.Done(key)
	} else {
		wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
			cloudPrivateIPConfig.Status = c.releaseStatusAssignment(cloudPrivateIPConfig)
			return len(cloudPrivateIPConfig.Status.Items) == 0, nil
		})
	}
}

func (c *CloudPrivateIPConfigController) releaseStatusAssignment(cloudPrivateIPConfig cloudprivateipconfig.CloudPrivateIPConfig) cloudprivateipconfig.CloudPrivateIPConfigStatus {
	remainingStatus := cloudprivateipconfig.CloudPrivateIPConfigStatus{}
	for _, privateIPStatus := range cloudPrivateIPConfig.Status.Items {
		ip := net.ParseIP(privateIPStatus.IP)
		if node, err := c.nodeInformer.Get(privateIPStatus.Node); err == nil {
			if err := c.CloudProviderClient.ReleasePrivateIP(ip, node); err != nil {
				remainingStatus.Items = append(remainingStatus.Items, privateIPStatus)
			}
		}
	}
	return remainingStatus
}

// Since we're dealing with two sets here, we'll simply need to compute the complement between both,
// i.e: toAdd = spec - status,
//      toDel = status - spec
func (c *CloudPrivateIPConfigController) computeRequestDiff(cloudPrivateIPConfig *cloudprivateipconfig.CloudPrivateIPConfig) ([]cloudprivateipconfig.CloudPrivateIPConfigItem, []cloudprivateipconfig.CloudPrivateIPConfigItem) {
	toAdd := c.complement(cloudPrivateIPConfig.Spec.Items, cloudPrivateIPConfig.Status.Items)
	toDel := c.complement(cloudPrivateIPConfig.Status.Items, cloudPrivateIPConfig.Spec.Items)
	return toAdd, toDel
}

func (c *CloudPrivateIPConfigController) complement(a, b []cloudprivateipconfig.CloudPrivateIPConfigItem) []cloudprivateipconfig.CloudPrivateIPConfigItem {
	tmp := make(map[string]string)
	for _, item := range b {
		tmp[c.getKey(item)] = ""
	}
	diff := []cloudprivateipconfig.CloudPrivateIPConfigItem{}
	for _, item := range a {
		if _, exists := tmp[c.getKey(item)]; !exists {
			diff = append(diff, item)
		}
	}
	return diff
}

func (c *CloudPrivateIPConfigController) isValidSpec(items []cloudprivateipconfig.CloudPrivateIPConfigItem) error {
	tmp := map[string]bool{}
	// Validate only unique items in []cloudprivateipconfig.CloudPrivateIPConfigItem
	for _, item := range items {
		if _, exist := tmp[c.getKey(item)]; exist {
			return fmt.Errorf("duplicate items found in the .spec stanza")
		}
		tmp[c.getKey(item)] = true
	}
	// Validate that no IP is assigned to multiple nodes or incorrectly defined
	tmp = map[string]bool{}
	for _, item := range items {
		if ip := net.ParseIP(item.IP); ip == nil {
			return fmt.Errorf("invalid IP address found in the .spec stanza: %s", item.IP)
		}
		if _, exist := tmp[item.IP]; exist {
			return fmt.Errorf("IP: %s has been defined on multiple nodes", item.IP)
		}
		tmp[item.IP] = true
	}
	return nil
}

func (c *CloudPrivateIPConfigController) getKey(item cloudprivateipconfig.CloudPrivateIPConfigItem) string {
	return fmt.Sprintf("%s/%s", item.Node, item.IP)
}
