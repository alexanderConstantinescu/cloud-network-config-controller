package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"

	cloudnetworkv1 "github.com/openshift/api/cloudnetwork/v1"
	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	cloudnetworkscheme "github.com/openshift/client-go/cloudnetwork/clientset/versioned/scheme"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions/cloudnetwork/v1"
	cloudnetworklisters "github.com/openshift/client-go/cloudnetwork/listers/cloudnetwork/v1"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	// cloudPrivateIPConfigControllerAgentType is the CloudPrivateIPConfig controller's dedicated resource type
	cloudPrivateIPConfigControllerAgentType reflect.Type = reflect.TypeOf(&cloudnetworkv1.CloudPrivateIPConfig{})
	// cloudPrivateIPConfigControllerAgentName is the controller name for the CloudPrivateIPConfig controller
	cloudPrivateIPConfigControllerAgentName = "cloud-private-ip-config"
	// cloudPrivateIPConfigFinalizer is the name of the finalizer blocking
	// object deletion until the cloud confirms that the IP has been removed
	cloudPrivateIPConfigFinalizer = "cloudprivateipconfig.cloud.network.openshift.io/finalizer"
	// cloudResponseReasonPending indicates a pending response from the cloud API
	cloudResponseReasonPending = "CloudResponsePending"
	// cloudResponseReasonError indicates an error response from the cloud API
	cloudResponseReasonError = "CloudResponseError"
	// cloudResponseReasonSuccess indicates a successful response from the cloud API
	cloudResponseReasonSuccess = "CloudResponseSuccess"
)

// CloudPrivateIPConfigController is the controller implementation for CloudPrivateIPConfig resources
type CloudPrivateIPConfigController struct {
	// Implements its own Node lister
	nodesLister corelisters.NodeLister
	// CloudProviderClient is a client interface allowing the controller
	// access to the cloud API
	CloudProviderClient cloudprovider.CloudProviderIntf
	// Implements its own lister and clientset for its own API group
	cloudNetworkClientset      cloudnetworkclientset.Interface
	cloudPrivateIPConfigLister cloudnetworklisters.CloudPrivateIPConfigLister
}

// NewCloudPrivateIPConfigController returns a new CloudPrivateIPConfig controller
func NewCloudPrivateIPConfigController(
	cloudProviderClient cloudprovider.CloudProviderIntf,
	cloudNetworkClientset cloudnetworkclientset.Interface,
	cloudPrivateIPConfigInformer cloudnetworkinformers.CloudPrivateIPConfigInformer,
	nodeInformer coreinformers.NodeInformer) *controller.CloudNetworkConfigController {

	utilruntime.Must(cloudnetworkscheme.AddToScheme(scheme.Scheme))

	cloudPrivateIPConfigController := &CloudPrivateIPConfigController{
		nodesLister:                nodeInformer.Lister(),
		CloudProviderClient:        cloudProviderClient,
		cloudNetworkClientset:      cloudNetworkClientset,
		cloudPrivateIPConfigLister: cloudPrivateIPConfigInformer.Lister(),
	}
	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{cloudPrivateIPConfigInformer.Informer().HasSynced, nodeInformer.Informer().HasSynced},
		cloudPrivateIPConfigController,
		cloudPrivateIPConfigControllerAgentName,
		cloudPrivateIPConfigControllerAgentType,
	)

	cloudPrivateIPConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.Enqueue,
		UpdateFunc: func(old, new interface{}) {
			oldCloudPrivateIPConfig, _ := old.(*cloudnetworkv1.CloudPrivateIPConfig)
			newCloudPrivateIPConfig, _ := new.(*cloudnetworkv1.CloudPrivateIPConfig)
			// Enqueue consumer updates and deletion. Given the presence of our
			// finalizer a delete action will be treated as an update before our
			// finalizer is removed, once the finalizer has been removed by this
			// controller we will receive the delete. We can be notified of this
			// by checking that the deletion timestamp has been set and
			// verifying the existence of the finalizer
			if (!newCloudPrivateIPConfig.GetDeletionTimestamp().IsZero() &&
				controllerutil.ContainsFinalizer(newCloudPrivateIPConfig, cloudPrivateIPConfigFinalizer)) ||
				!reflect.DeepEqual(oldCloudPrivateIPConfig.Spec, newCloudPrivateIPConfig.Spec) {
				controller.Enqueue(new)
			}
		},
		DeleteFunc: controller.Enqueue,
	})
	return controller
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the CloudPrivateIPConfig
// resource with the current status of the resource.
// On update: we should only process the add once we've received the cloud's answer
// for the delete. We risk having the IP address being assigned to two nodes at
// the same time otherwise.

// We have two "data stores": kube API server and the cloud API. Thus, there are
// two main error conditions:

// - We couldn't update the object in the kube API, but did update the object in the cloud.
// - We couldn't update the object in the cloud, but did update the object in the kube API.

// - If we couldn't update either, we just resync the original object
// - If we could update both, we don't resync the object

//  Here's a schema of CloudPrivateIPConfig's reconciliation loop based on the consumer input:

// - ADD:
// 1. 	Send cloud API ADD request
// 2. 	Set status.node = spec.node && status.conditions[0].Status = Pending
// ...some time later
// 3.	Get cloud API ADD result
// * 	If OK: set status.conditions[0].Status = Success
// *	If !OK: set status.node == "" && set status.conditions[0].Status = Error && goto 1. by resync

// - DELETE:
// 1.	Send cloud API DELETE request
// 2. 	Set status.conditions[0].Status = Pending (keep old status.node)
// ...some time later
// 3. 	Get cloud API DELETE result
// *	If OK: -
// * 	If !OK: set status.conditions[0].Status = Error && goto 1. by resync

// - UPDATE:
// 1.	goto DELETE
// *	If OK: status.node = "" && goto ADD

// Consumer should only consider ADD / UPDATE successful when:
// - 	spec.node == status.node && status.conditions[0].Status == Success
func (c *CloudPrivateIPConfigController) SyncHandler(key string) error {

	var status *cloudnetworkv1.CloudPrivateIPConfigStatus
	var cloudRequestObj interface{}
	klog.Infof("Processing key: %s from CloudPrivateIPConfig work queue", key)

	// Convert the key into a distinct name (since CloudPrivateIPConfig is
	// cluster-scoped)
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// This object will recursively be updated during this sync
	cloudPrivateIPConfig, err := c.cloudPrivateIPConfigLister.Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			// The object was deleted while we were processing the request,
			// nothing more to do, the finalizer portion of this sync will
			// handle any last cleanup
			utilruntime.HandleError(fmt.Errorf("CloudPrivateIPConfig: '%s' in work queue no longer exists", name))
			return nil
		}
		return fmt.Errorf("Error retrieving CloudPrivateIPConfig: %s from the API server, err: %v", name, err)
	}

	nodeToAdd, nodeToDel := c.computeOp(cloudPrivateIPConfig)

	// Dequeue on NOOP, there's nothing to do
	if nodeToAdd == "" && nodeToDel == "" {
		return nil
	}

	if nodeToDel != "" {

		klog.Infof("CloudPrivateIPConfig: %s will be deleted from node: %s", name, nodeToDel)
		ip := net.ParseIP(cloudPrivateIPConfig.Name)

		node, err := c.nodesLister.Get(nodeToDel)
		if err != nil {
			return fmt.Errorf("corev1.Node: %s could not be retrieved from the API server, err: %v", node.Name, err)
		}

		if cloudRequestObj, err = c.CloudProviderClient.ReleasePrivateIP(ip, node); err != nil {
			// Delete operation encountered an error, requeue
			status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
				Node: cloudPrivateIPConfig.Status.Node,
				Conditions: []metav1.Condition{
					metav1.Condition{
						Type:               string(cloudnetworkv1.Assigned),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
						LastTransitionTime: metav1.Now(),
						Reason:             cloudResponseReasonError,
						Message:            fmt.Sprintf("Error issuing cloud release request, err: %v", err),
					},
				},
			}
			// Always requeue the object if we end up here. We need to make sure
			// we try to clean up the IP on the cloud
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
				return err
			}); err != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s during delete operation, err: %v", name, err)
			}
			return fmt.Errorf("CloudPrivateIPConfig: %s could not be released from node: %s, err: %v", name, node.Name, err)
		}
		// This is step 2. in the docbloc for the DELETE operation in the
		// syncHandler
		status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
			Node: cloudPrivateIPConfig.Status.Node,
			Conditions: []metav1.Condition{
				metav1.Condition{
					Type:               string(cloudnetworkv1.Assigned),
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
					LastTransitionTime: metav1.Now(),
					Reason:             cloudResponseReasonPending,
				},
			},
		}
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
			return err
		}); err != nil {
			return fmt.Errorf("Error updating CloudPrivateIPConfig: %s during delete operation, err: %v", name, err)
		}
		// This is a long running and blocking function call.
		cloudErr := c.CloudProviderClient.WaitForResponse(cloudRequestObj)
		// Process real object deletion. We're using a finalizer, so it depends
		// on this controller whether the object is finally deleted and removed
		// from the store or not, hence don't check the store.
		if !cloudPrivateIPConfig.ObjectMeta.DeletionTimestamp.IsZero() {
			klog.Infof("CloudPrivateIPConfig: %s object has been marked for complete deletion", name)
			if controllerutil.ContainsFinalizer(cloudPrivateIPConfig, cloudPrivateIPConfigFinalizer) {
				// Everything has been cleaned up, remove the finalizer from the
				// object and update so that the object gets removed. If it
				// didn't get removed and we encountered an error we'll requeue
				// it down below
				if cloudErr == nil {
					controllerutil.RemoveFinalizer(cloudPrivateIPConfig, cloudPrivateIPConfigFinalizer)
					klog.Infof("Cleaning up IP address and finalizer for CloudPrivateIPConfig: %s, deleting it completely", name)
					return retry.RetryOnConflict(retry.DefaultRetry, func() error {
						_, err = c.updateCloudPrivateIPConfig(cloudPrivateIPConfig)
						return err
					})
				}
			}
		}
		if cloudErr != nil {
			// Delete operation encountered an error, requeue
			status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
				Node: cloudPrivateIPConfig.Status.Node,
				Conditions: []metav1.Condition{
					metav1.Condition{
						Type:               string(cloudnetworkv1.Assigned),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
						LastTransitionTime: metav1.Now(),
						Reason:             cloudResponseReasonError,
						Message:            fmt.Sprintf("Error processing cloud request, err: %v", err),
					},
				},
			}
			// Always requeue the object if we end up here. We need to make sure
			// we try to clean up the IP on the cloud
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
				return err
			}); err != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s during delete operation, err: %v", name, err)
			}
			return fmt.Errorf("Error deleting IP address from node: %s for CloudPrivateIPConfig: %s, cloud err: %v", node.Name, name, cloudErr)
		}

		klog.Infof("Deleted IP address from node: %s for CloudPrivateIPConfig: %s", node.Name, name)
		if nodeToAdd != "" {
			// Update the status here if we process an update so that it's
			// evident to the consumer where we are in our sync and so that we
			// can treat the remainder as an add in the next sync term, in case
			// we fail from this moment on. If we only process a delete we'll
			// update at the bottom.
			status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
				Conditions: []metav1.Condition{
					metav1.Condition{
						Type:               string(cloudnetworkv1.Assigned),
						Status:             metav1.ConditionUnknown,
						ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
						LastTransitionTime: metav1.Now(),
						Reason:             cloudResponseReasonPending,
					},
				},
			}
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
				return err
			}); err != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s during delete operation, err: %v", name, err)
			}
		}
	}

	if nodeToAdd != "" {

		klog.Infof("CloudPrivateIPConfig: %s will be added to node: %s", name, nodeToAdd)
		ip := net.ParseIP(cloudPrivateIPConfig.Name)

		node, err := c.nodesLister.Get(nodeToAdd)
		if err != nil {
			return fmt.Errorf("corev1.Node: %s could not be retrieved from the API server, err: %v", node.Name, err)
		}

		// If the object is new there won't be a generation set, so initialize
		// it to 0
		generation := int64(0)
		if len(cloudPrivateIPConfig.Status.Conditions) > 0 {
			generation = cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration
		}

		if cloudRequestObj, err = c.CloudProviderClient.AssignPrivateIP(ip, node); err != nil {
			if err == cloudprovider.AlreadyExistingIPError {
				// If the IP is assigned (for ex: in case we were killed during
				// the last sync but managed sending the cloud request away
				// prior to that) then just update the status to reflect that.
				klog.Warningf("CloudPrivateIPConfig: %s is already assigned to node: %s, updating the status to reflect this", name, node.Name)
				status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: cloudPrivateIPConfig.Spec.Node,
					Conditions: []metav1.Condition{
						metav1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: generation + 1,
							LastTransitionTime: metav1.Now(),
							Reason:             cloudResponseReasonSuccess,
							Message:            "",
						},
					},
				}
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
					return err
				}); err != nil {
					return fmt.Errorf("Error updating CloudPrivateIPConfig: %s status for AlreadyExistingIPError, err: %v", name, err)
				}
				return nil
			}
			// If we couldn't even execute the assign request, set the status to
			// failed.
			status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
				Conditions: []metav1.Condition{
					metav1.Condition{
						Type:               string(cloudnetworkv1.Assigned),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: generation + 1,
						LastTransitionTime: metav1.Now(),
						Reason:             cloudResponseReasonError,
						Message:            fmt.Sprintf("Error issuing cloud assignment request, err: %v", err),
					},
				},
			}
			if updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
				return err
			}); updateErr != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s status for error issuing cloud assignment, err: %v", name, updateErr)
			}
			return fmt.Errorf("Error assigning CloudPrivateIPConfig: %s to node: %s, err: %v", name, node.Name, err)
		}

		// This is step 2. in the docbloc for the ADD operation in the
		// syncHandler
		status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
			Node: cloudPrivateIPConfig.Spec.Node,
			Conditions: []metav1.Condition{
				metav1.Condition{
					Type:               string(cloudnetworkv1.Assigned),
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: generation + 1,
					LastTransitionTime: metav1.Now(),
					Reason:             cloudResponseReasonPending,
				},
			},
		}
		// Add the finalizer now so that the object can't be removed from under
		// us while we process the cloud's answer
		if !controllerutil.ContainsFinalizer(cloudPrivateIPConfig, cloudPrivateIPConfigFinalizer) {
			klog.Infof("Adding finalizer to CloudPrivateIPConfig: %s", name)
			controllerutil.AddFinalizer(cloudPrivateIPConfig, cloudPrivateIPConfigFinalizer)
			// If we can't update the object here, return an error. It will will
			// retry the last state of the object, meaning: we should end up here
			// again
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfig(cloudPrivateIPConfig)
				return err
			}); err != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s, err: %v", name, err)
			}
		}

		// This is annoying, but we need two updates here since we're adding a
		// finalizer. One update for the status and one for the object. The
		// reason for this is because we've defined:
		// +kubebuilder:subresource:status on the CRD marking the status as
		// impossible to update for anything/anyone else except for this
		// controller.
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
			return err
		}); err != nil {
			return fmt.Errorf("Error updating CloudPrivateIPConfig: %s, err: %v", name, err)
		}
		// This is a long running and blocking function call. There's no worry
		// if the consumer updates the spec while this is happening, since the
		// updated object will be queued in the next sync and we should be
		// cleaning this up and proceeding to updating to whatever new node
		// might be defined during that term. No consumer is allowed to update
		// the status since the CRD is marked as
		// +kubebuilder:subresource:status)
		cloudErr := c.CloudProviderClient.WaitForResponse(cloudRequestObj)
		if cloudErr != nil {
			// Add encountered error, requeue
			status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
				Conditions: []metav1.Condition{
					metav1.Condition{
						Type:               string(cloudnetworkv1.Assigned),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
						LastTransitionTime: metav1.Now(),
						Reason:             cloudResponseReasonError,
						Message:            fmt.Sprintf("Error processing cloud request, err: %v", err),
					},
				},
			}
			// Always requeue the object if we end up here. We need to make sure
			// we try to re-add the IP
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				cloudPrivateIPConfig, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
				return err
			}); err != nil {
				return fmt.Errorf("Error updating CloudPrivateIPConfig: %s during ADD operation, err: %v", name, err)
			}
			return fmt.Errorf("Error adding IP address to node: %s for CloudPrivateIPConfig: %s, cloud err: %v", node.Name, name, cloudErr)
		}
		// Add occurred and no error was encountered, keep status.node from
		// above
		status = &cloudnetworkv1.CloudPrivateIPConfigStatus{
			Node: cloudPrivateIPConfig.Status.Node,
			Conditions: []metav1.Condition{
				metav1.Condition{
					Type:               string(cloudnetworkv1.Assigned),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: cloudPrivateIPConfig.Status.Conditions[0].ObservedGeneration + 1,
					LastTransitionTime: metav1.Now(),
					Reason:             cloudResponseReasonSuccess,
				},
			},
		}
		klog.Infof("Added IP address to node: %s for CloudPrivateIPConfig: %s", node.Name, name)
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err = c.updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig, status)
		return err
	})
}

// updateCloudPrivateIPConfigStatus copies and updates the provided object and returns
// the new object. The return value can be useful for recursive updates
func (c *CloudPrivateIPConfigController) updateCloudPrivateIPConfigStatus(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig, status *cloudnetworkv1.CloudPrivateIPConfigStatus) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	cloudPrivateIPConfigCopy := cloudPrivateIPConfig.DeepCopy()
	cloudPrivateIPConfigCopy.Status = *status
	return c.cloudNetworkClientset.CloudV1().CloudPrivateIPConfigs().UpdateStatus(context.TODO(), cloudPrivateIPConfigCopy, metav1.UpdateOptions{})
}

// updateCloudPrivateIPConfig copies and updates the provided object and returns
// the new object. The return value can be useful for recursive updates
func (c *CloudPrivateIPConfigController) updateCloudPrivateIPConfig(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig) (*cloudnetworkv1.CloudPrivateIPConfig, error) {
	cloudPrivateIPConfigCopy := cloudPrivateIPConfig.DeepCopy()
	return c.cloudNetworkClientset.CloudV1().CloudPrivateIPConfigs().Update(context.TODO(), cloudPrivateIPConfigCopy, metav1.UpdateOptions{})
}

// computeOp decides on what needs to be done given the state of the object.
func (c *CloudPrivateIPConfigController) computeOp(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig) (string, string) {
	// Delete if the deletion timestamp is set and we still have our finalizer listed
	if !cloudPrivateIPConfig.GetDeletionTimestamp().IsZero() && controllerutil.ContainsFinalizer(cloudPrivateIPConfig, cloudPrivateIPConfigFinalizer) {
		return "", cloudPrivateIPConfig.Status.Node
	}
	// Update if status and spec are different
	if cloudPrivateIPConfig.Spec.Node != cloudPrivateIPConfig.Status.Node {
		return cloudPrivateIPConfig.Spec.Node, cloudPrivateIPConfig.Status.Node
	}
	// Add if the status is un-assigned or if the status is marked failed
	if cloudPrivateIPConfig.Status.Node == "" || cloudPrivateIPConfig.Status.Conditions[0].Status != metav1.ConditionTrue {
		return cloudPrivateIPConfig.Spec.Node, ""
	}
	// Default to NOOP
	return "", ""
}
