package controller

import (
	"fmt"
	"time"

	"github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

var (
	// NodeControllerAgentName is the controller name for the Node controller
	NodeControllerAgentName = "node"
	// CloudPrivateIPConfigControllerAgentName is the controller name for the CloudPrivateIPConfig controller
	CloudPrivateIPConfigControllerAgentName = "cloud-private-ip-config"
)

type CloudNetworkConfigControllerIntf interface {
	Run(threadiness int, stopCh <-chan struct{}) error
	runWorker()
	processNextWorkItem() bool
	syncHandler(key string) error
}

type CloudNetworkConfigController struct {
	// CloudNetworkConfigController implements the generic interface:
	// CloudNetworkConfigControllerIntf, which allows all derived
	// controllers an abstraction from the "bricks and pipes" of the
	// controller framework, allowing them to implement only their
	// specific control loop functionality and not bother with the rest.
	CloudNetworkConfigControllerIntf
	// KubeClientset is a standard kubernetes clientset
	KubeClientset kubernetes.Interface
	// CloudProviderClient is a client interface allowing the controller
	// access to the cloud API
	CloudProviderClient *cloudprovider.CloudProvider
	// Workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	Workqueue workqueue.RateLimitingInterface
	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder
	// Synced contains all required resource informers for a controller
	// to run
	Synced []cache.InformerSynced
	// controllerKey is an internal key used for the Workqueue and
	// recorder
	controllerKey string
}

func NewCloudNetworkConfigController(
	kubeclientset kubernetes.Interface,
	cloudProviderClient *cloudprovider.CloudProvider,
	controllerKey string,
	syncs []cache.InformerSynced) CloudNetworkConfigController {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerKey})

	return CloudNetworkConfigController{
		KubeClientset:       kubeclientset,
		CloudProviderClient: cloudProviderClient,
		Workqueue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerKey),
		Recorder:            recorder,
		Synced:              syncs,
		controllerKey:       controllerKey,
	}
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *CloudNetworkConfigController) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.Workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Infof("Starting %s controller", c.controllerKey)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.Synced...); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *CloudNetworkConfigController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *CloudNetworkConfigController) processNextWorkItem() bool {
	obj, shutdown := c.Workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.Workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.Workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.Workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.Workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}
