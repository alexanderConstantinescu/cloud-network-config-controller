package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
)

var (
	// nodeControllerAgentType is the Node controller's dedicated resource type
	nodeControllerAgentType reflect.Type = reflect.TypeOf(&corev1.Node{})
	// nodeControllerAgentName is the controller name for the Node controller
	nodeControllerAgentName = "node"
	// nodeCloudIfAddrAnnoationKey is the annotation key used for indicating the node's cloud subnet
	nodeCloudIfAddrAnnoationKey = "cloud.network.openshift.io/cloud-if-addr"
)

// NodeController is the controller implementation for Node resources
// This controller is used to annotate nodes for the purposes of the
// cloud network config controller
type NodeController struct {
	// Implements its own Node lister
	nodesLister corelisters.NodeLister
	// CloudProviderClient is a client interface allowing the controller
	// access to the cloud API
	CloudProviderClient cloudprovider.CloudProviderIntf
	// KubeClientset is a standard kubernetes clientset
	KubeClientset kubernetes.Interface
}

// NewNodeController returns a new Node controller
func NewNodeController(
	kubeClientset kubernetes.Interface,
	cloudProviderClient cloudprovider.CloudProviderIntf,
	nodeInformer coreinformers.NodeInformer) *controller.CloudNetworkConfigController {

	nodeController := &NodeController{
		nodesLister:         nodeInformer.Lister(),
		KubeClientset:       kubeClientset,
		CloudProviderClient: cloudProviderClient,
	}

	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{nodeInformer.Informer().HasSynced},
		nodeController,
		nodeControllerAgentName,
		nodeControllerAgentType,
	)

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.Enqueue,
	})
	return controller
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Node resource
// with the current status of the resource.
func (n *NodeController) SyncHandler(key string) error {
	// Convert the key to a name (since Node is non-namespaced)
	klog.Infof("Processing key: %s from corev1.Node work queue", key)
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	node, err := n.nodesLister.Get(name)
	if err != nil {
		// The Node resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("corev1.Node: '%s' in work queue no longer exists", key))
			return nil
		}
		return fmt.Errorf("error retrieving corev1.Node from the API server, err: %v", err)
	}
	// If the node already has the annotation (ex: if we restart it is expected that
	// the nodes would) we skip it. Subnets won't change.
	annotations := node.GetAnnotations()
	if _, ok := annotations[nodeCloudIfAddrAnnoationKey]; ok {
		return nil
	}
	v4Subnet, v6Subnet, err := n.CloudProviderClient.GetNodeSubnet(node)
	if err != nil {
		return fmt.Errorf("error retrieving node subnet for node: %s, err: %v", node.GetName(), err)
	}
	klog.Infof("Setting annotation: '%s' on node: %s with IPv4 subnet: %v / IPv6 subnet: %v", nodeCloudIfAddrAnnoationKey, node.Name, v4Subnet, v6Subnet)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return n.SetCloudSubnetAnnotationOnNode(node, v4Subnet, v6Subnet)
	})
}

// SetCloudSubnetAnnotationOnNode annotates corev1.Node with the cloud subnet information
func (n *NodeController) SetCloudSubnetAnnotationOnNode(node *corev1.Node, v4Subnet, v6Subnet *net.IPNet) error {
	annotation, err := n.generateAnnotation(v4Subnet, v6Subnet)
	if err != nil {
		return err
	}

	nodeCopy := node.DeepCopy()
	existingAnnotations := nodeCopy.GetAnnotations()
	existingAnnotations[nodeCloudIfAddrAnnoationKey] = annotation
	nodeCopy.SetAnnotations(existingAnnotations)

	_, err = n.KubeClientset.CoreV1().Nodes().Update(context.TODO(), nodeCopy, metav1.UpdateOptions{})
	return err
}

type cloudIfAddrAnnotation struct {
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
}

func (n *NodeController) generateAnnotation(v4Subnet, v6Subnet *net.IPNet) (string, error) {
	cloudIfAddrAnnotation := cloudIfAddrAnnotation{}
	if v4Subnet != nil {
		cloudIfAddrAnnotation.IPv4 = v4Subnet.String()
	}
	if v6Subnet != nil {
		cloudIfAddrAnnotation.IPv6 = v6Subnet.String()
	}
	serialized, err := json.Marshal(cloudIfAddrAnnotation)
	if err != nil {
		return "", fmt.Errorf("error serializing cloud subnet annotation, err: %v", err)
	}
	return string(serialized), nil
}
