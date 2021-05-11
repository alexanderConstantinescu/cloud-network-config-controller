package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
)

var (
	// secretControllerAgentType is the Secret controller's dedicated resource type
	secretControllerAgentType reflect.Type = reflect.TypeOf(&corev1.Secret{})
	// secretControllerAgentName is the controller name for the Secret controller
	secretControllerAgentName = "secret"
)

// SecretController is the controller implementation for Secret resources
// This controller is used to watch for secret rotations by the cloud-
// credentials-operator for what concerns the cloud API secret
type SecretController struct {
	// Implements its own Secret lister
	secretLister corelisters.SecretLister
	// controllerCancel is the components global cancelFunc. This one is used to
	// cancel the global context, stop the leader election and subsequently
	// initiate a shut down of all control loops
	controllerCancel context.CancelFunc
}

// NewSecretController returns a new Secret controller
func NewSecretController(
	controllerCancel context.CancelFunc,
	kubeClientset kubernetes.Interface,
	secretInformer coreinformers.SecretInformer,
	secretName, secretNamespace string) *controller.CloudNetworkConfigController {

	secretController := &SecretController{
		secretLister:     secretInformer.Lister(),
		controllerCancel: controllerCancel,
	}

	controller := controller.NewCloudNetworkConfigController(
		[]cache.InformerSynced{secretInformer.Informer().HasSynced},
		secretController,
		secretControllerAgentName,
		secretControllerAgentType,
	)

	secretFilter := func(obj interface{}) bool {
		if secret, ok := obj.(*corev1.Secret); ok {
			return secret.Name == secretName && secret.Namespace == secretNamespace
		}
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if secret, ok := tombstone.Obj.(*corev1.Secret); ok {
				return secret.Name == secretName && secret.Namespace == secretNamespace
			}
		}
		return false
	}

	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: secretFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			// Only handle updates and deletes
			//  - Add events can be avoided since the secret should already be
			// mounted for us to even start.
			UpdateFunc: func(old, new interface{}) {
				oldSecret, _ := old.(*corev1.Secret)
				newSecret, _ := new.(*corev1.Secret)

				// Don't process resync or objects that are marked for deletion
				if oldSecret.ResourceVersion == newSecret.ResourceVersion ||
					!newSecret.GetDeletionTimestamp().IsZero() {
					return
				}

				// Only enqueue on data change
				if !reflect.DeepEqual(oldSecret.Data, newSecret.Data) {
					controller.Enqueue(new)
				}
			},
			DeleteFunc: controller.Enqueue,
		},
	})
	return controller
}

// syncHandler does not compare the actual state with the desired, it's
// triggered on a secret.data change and cancels the global context forcing us
// to re-initialize the cloud credentials on restart.
func (s *SecretController) SyncHandler(key string) error {
	// Convert the key to a name/namespace
	klog.Infof("Processing key: %s from corev1.Secret work queue", key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	_, err = s.secretLister.Secrets(namespace).Get(name)
	if err != nil {
		// The Secret resource may no longer exist, in which case we proceed
		// to killing the process one final time. If the secret is deleted
		// we should not be able to restart since we won't be able to mount
		// the secret location upon restart.
		if errors.IsNotFound(err) {
			s.shutdown()
			return nil
		}
		return fmt.Errorf("error retrieving corev1.Secret from the API server, err: %v", err)
	}
	s.shutdown()
	return nil
}

// shutdown is called in case we hit a secret rotation. We need to: process all
// in-flight requests and pause all our controllers for any further ones (since
// we can't communicate with the cloud API using the old data anymore). I don't
// know what the "Kubernetes-y" thing to do is, but it seems like cancelling the
// global context and subsequently sending a SIGTERM will do just that.
func (s *SecretController) shutdown() {
	klog.Info("Re-initializing cloud API credentials, cancelling controller context")
	s.controllerCancel()
}
