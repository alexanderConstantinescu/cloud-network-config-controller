package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controllers"
)

const (
	nodeCloudIfAddrAnnoationKey = "network.openshift.io/cloud-if-addr"
)

// NodeController is the controller implementation for Node resources
// This controller is used to annotate nodes for the purposes of the
// cloud network config controller
type NodeController struct {
	// "Extends" our generic controller
	controller.CloudNetworkConfigController
	// Implements its own Node lister
	nodesLister corelisters.NodeLister
}

// NewNodeController returns a new Node controller
func NewNodeController(
	kubeClientset kubernetes.Interface,
	cloudProviderClient *cloudprovider.CloudProvider,
	nodeInformer coreinformers.NodeInformer) *NodeController {

	cloudNetworkConfigController := controller.NewCloudNetworkConfigController(
		kubeClientset,
		cloudProviderClient,
		controller.NodeControllerAgentName,
		[]cache.InformerSynced{nodeInformer.Informer().HasSynced},
	)

	controller := &NodeController{
		CloudNetworkConfigController: cloudNetworkConfigController,
		nodesLister:                  nodeInformer.Lister(),
	}

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueNode,
	})
	return controller
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Node resource
// with the current status of the resource.
func (c *NodeController) syncHandler(key string) error {
	// Convert the key to a name (since Node is non-namespaced)
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	node, err := c.nodesLister.Get(name)
	if err != nil {
		// The Node resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("Node: '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}
	v4Subnet, v6Subnet, err := c.CloudProviderClient.GetNodeSubnet(node)
	if err != nil {
		return err
	}
	return c.SetCloudSubnetAnnotationOnNode(node, v4Subnet, v6Subnet)
}

// enqueueNode takes a Node resource and converts it into a name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Node.
func (c *NodeController) enqueueNode(obj interface{}) {
	var key string
	var err error
	if _, isObjectTypeCorrect := obj.(corev1.Node); !isObjectTypeCorrect {
		utilruntime.HandleError(fmt.Errorf("spurious object detected, not of type: corev1.Node"))
		return
	}
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.Workqueue.Add(key)
}

type cloudIfAddrAnnotation struct {
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
}

// SetCloudSubnetAnnotationOnNode annotates corev1.Node with the cloud subnet information
func (c *NodeController) SetCloudSubnetAnnotationOnNode(node *corev1.Node, v4Subnet, v6Subnet *net.IPNet) error {
	cloudIfAddrAnnotation := cloudIfAddrAnnotation{}
	if v4Subnet != nil {
		cloudIfAddrAnnotation.IPv4 = v4Subnet.String()
	}
	if v6Subnet != nil {
		cloudIfAddrAnnotation.IPv6 = v6Subnet.String()
	}
	patch := struct {
		Metadata map[string]interface{} `json:"metadata"`
	}{
		Metadata: map[string]interface{}{
			"annotations": map[string]interface{}{
				nodeCloudIfAddrAnnoationKey: cloudIfAddrAnnotation,
			},
		},
	}
	patchData, err := json.Marshal(&patch)
	if err != nil {
		return err
	}
	_, err = c.KubeClientset.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.MergePatchType, patchData, metav1.PatchOptions{})
	return err
}
