package controller

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	cloudnetworkv1 "github.com/openshift/api/cloudnetwork/v1"
	fakecloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned/fake"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	controller "github.com/openshift/cloud-network-config-controller/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	cloudPrivateIPConfigName = "192.168.172.12"
	nodeNameA                = "nodeA"
	nodeNameB                = "nodeB"
	nodeNameC                = "nodeC"
)

type FakeCloudPrivateIPConfigController struct {
	*controller.CloudNetworkConfigController
	kubeClient                *fakekubeclient.Clientset
	cloudNetworkClient        *fakecloudnetworkclientset.Clientset
	cloudProvider             *cloudprovider.FakeCloudProvider
	cloudPrivateIPConfigStore cache.Store
	nodeStore                 cache.Store
}

func (f *FakeCloudPrivateIPConfigController) initTestSetup(cloudPrivateIPConfig *cloudnetworkv1.CloudPrivateIPConfig) {
	f.cloudPrivateIPConfigStore.Add(cloudPrivateIPConfig)
	f.nodeStore.Add(&corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: nodeNameA,
		},
	})
	f.nodeStore.Add(&corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: nodeNameB,
		},
	})
	f.nodeStore.Add(&corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: nodeNameC,
		},
	})
}

type CloudPrivateIPConfigTestCase struct {
	name                               string
	mockCloudAssignError               bool
	mockCloudAssignErrorWithExistingIP bool
	mockCloudReleaseError              bool
	mockCloudWaitError                 bool
	testObject                         *cloudnetworkv1.CloudPrivateIPConfig
	expectedObject                     *cloudnetworkv1.CloudPrivateIPConfig
	expectErrorOnSync                  bool
}

func (t *CloudPrivateIPConfigTestCase) NewFakeCloudPrivateIPConfigController() *FakeCloudPrivateIPConfigController {

	fakeCloudNetworkClient := fakecloudnetworkclientset.NewSimpleClientset([]runtime.Object{t.testObject}...)
	fakeKubeClient := fakekubeclient.NewSimpleClientset()
	fakeCloudProvider := cloudprovider.NewFakeCloudProvider(t.mockCloudAssignError, t.mockCloudAssignErrorWithExistingIP, t.mockCloudReleaseError, t.mockCloudWaitError)

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeKubeClient, 0)
	cloudNetworkInformerFactory := cloudnetworkinformers.NewSharedInformerFactory(fakeCloudNetworkClient, 0)

	cloudPrivateIPConfigController := NewCloudPrivateIPConfigController(
		fakeCloudProvider,
		fakeCloudNetworkClient,
		cloudNetworkInformerFactory.Cloud().V1().CloudPrivateIPConfigs(),
		kubeInformerFactory.Core().V1().Nodes(),
	)

	fakeCloudPrivateIPConfigController := &FakeCloudPrivateIPConfigController{
		CloudNetworkConfigController: cloudPrivateIPConfigController,
		kubeClient:                   fakeKubeClient,
		cloudNetworkClient:           fakeCloudNetworkClient,
		cloudProvider:                fakeCloudProvider,
		cloudPrivateIPConfigStore:    cloudNetworkInformerFactory.Cloud().V1().CloudPrivateIPConfigs().Informer().GetStore(),
		nodeStore:                    kubeInformerFactory.Core().V1().Nodes().Informer().GetStore(),
	}

	fakeCloudPrivateIPConfigController.initTestSetup(t.testObject)

	return fakeCloudPrivateIPConfigController
}

func assertSyncedExpectedObjectsEqual(synced, expected *cloudnetworkv1.CloudPrivateIPConfig) error {
	if len(synced.Status.Conditions) != len(expected.Status.Conditions) {
		return fmt.Errorf("synced object does not have expected status condition length, synced: %v, expected: %v", len(synced.Status.Conditions), len(expected.Status.Conditions))
	}
	if len(synced.Status.Conditions) == 0 {
		return nil
	}
	if synced.Status.Node != expected.Status.Node {
		return fmt.Errorf("synced object does not have expected node assignment, synced: %s, expected: %s", synced.Status.Node, expected.Status.Node)
	}
	if synced.Status.Conditions[0].Reason != expected.Status.Conditions[0].Reason {
		return fmt.Errorf("synced object does not have expected condition type, synced: %v, expected: %v", synced.Status.Conditions[0].Reason, expected.Status.Conditions[0].Reason)
	}
	if synced.Status.Conditions[0].Status != expected.Status.Conditions[0].Status {
		return fmt.Errorf("synced object does not have expected condition status, synced: %s, expected: %s", synced.Status.Conditions[0].Status, expected.Status.Conditions[0].Status)
	}
	if synced.Status.Conditions[0].ObservedGeneration != expected.Status.Conditions[0].ObservedGeneration {
		return fmt.Errorf("synced object does not have expected observed generation, synced: %v, expected: %v", synced.Status.Conditions[0].ObservedGeneration, expected.Status.Conditions[0].ObservedGeneration)
	}
	if !reflect.DeepEqual(synced.GetFinalizers(), expected.GetFinalizers()) {
		return fmt.Errorf("synced object does not have expected finalizers, synced: %v, expected: %v", synced.GetFinalizers(), expected.GetFinalizers())
	}
	return nil
}

// TestSyncCloudPrivateIPConfig tests sync state for our CloudPrivateIPConfig
// control loop. It does not test:
//  - that the node specified is valid - that is handled by the admission controller
//  - that the CloudPrivateIPConfig name is a valid IP - that is handled by OpenAPI
// Hence, all tests here are written with a valid spec. Moreover, this
// controller neither deletes nor creates objects. Hence the only Kubernetes
// action we need to verify is update, i.e: that the control loop updates the
// resource as expected during its sync.
func TestSyncAddCloudPrivateIPConfig(t *testing.T) {
	tests := []CloudPrivateIPConfigTestCase{
		{
			name: "Should be able to sync object on add without any errors",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionTrue,
							Reason: cloudResponseReasonSuccess,
							// One update for the assign and one for the
							// wait response
							ObservedGeneration: 2,
						},
					},
				},
			},
		},
		{
			name: "Should fail to sync object on add with assign error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the assign
							ObservedGeneration: 1,
						},
					},
				},
			},
			mockCloudAssignError: true,
			expectErrorOnSync:    true,
		},
		{
			name: "Should fail to sync object on add with wait error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the assign and one for the
							// wait response
							ObservedGeneration: 2,
						},
					},
				},
			},
			mockCloudWaitError: true,
			expectErrorOnSync:  true,
		},
		{
			name: "Should be able to re-sync object on add with AlreadyExistingIPError",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					// Node = "nodeNameA" means the object was processed as an
					// add during the last sync, but failed
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type: string(cloudnetworkv1.Assigned),
							// Fake a pending sync in the last term by setting an
							// unknown status. This would "IRL" mean that this
							// controller died while processing this object
							// during its last sync term, and now has restarted
							// and should re-sync it correctly.
							Status:             v1.ConditionUnknown,
							Reason:             cloudResponseReasonPending,
							ObservedGeneration: 5,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionTrue,
							Reason: cloudResponseReasonSuccess,
							// One update for the assign status
							ObservedGeneration: 6,
						},
					},
				},
			},
			mockCloudAssignError:               true,
			mockCloudAssignErrorWithExistingIP: true,
		},
		{
			name: "Should fail to re-sync object on add with assign error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					// Node = "nodeNameA" means the object was processed as an
					// add during the last sync, but failed
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type: string(cloudnetworkv1.Assigned),
							// Fake a pending sync in the last term by setting an
							// unknown status. This would "IRL" mean that this
							// controller died while processing this object
							// during its last sync term, and now has restarted
							// and should re-sync it correctly.
							Status:             v1.ConditionUnknown,
							Reason:             cloudResponseReasonPending,
							ObservedGeneration: 5,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the error status
							ObservedGeneration: 6,
						},
					},
				},
			},
			mockCloudAssignError: true,
			expectErrorOnSync:    true,
		},
		{
			name: "Should fail to re-sync object on add with wait error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					// Node = "nodeNameA" means the object was processed as an
					// add during the last sync, but didn't finish
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type: string(cloudnetworkv1.Assigned),
							// Fake a pending sync in the last term by setting an
							// unknown status. This would "IRL" mean that this
							// controller died while processing this object
							// during its last sync term, and now has restarted
							// and should re-sync it correctly.
							Status:             v1.ConditionUnknown,
							Reason:             cloudResponseReasonPending,
							ObservedGeneration: 5,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the assign and one for the error
							ObservedGeneration: 7,
						},
					},
				},
			},
			mockCloudWaitError: true,
			expectErrorOnSync:  true,
		},
		{
			name: "Should be able to re-sync object on add without any cloud errors",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type: string(cloudnetworkv1.Assigned),
							// Fake a failed sync in the last term by setting a
							// false status.
							Status:             v1.ConditionFalse,
							Reason:             cloudResponseReasonError,
							Message:            "Something bad happened during the last sync",
							ObservedGeneration: 5,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionTrue,
							Reason: cloudResponseReasonSuccess,
							// One update for the assign and one for wait
							// response
							ObservedGeneration: 7,
						},
					},
				},
			},
		},
	}
	runTests(t, tests)
}

func TestSyncDeleteCloudPrivateIPConfig(t *testing.T) {
	tests := []CloudPrivateIPConfigTestCase{
		{
			name: "Should be able to sync object on delete without any errors",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name:       cloudPrivateIPConfigName,
					Finalizers: []string{},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionUnknown,
							Reason: cloudResponseReasonPending,
							// One update for the release
							ObservedGeneration: 3,
						},
					},
				},
			},
		},
		{
			name: "Should fail to sync object on delete with release error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the release
							ObservedGeneration: 3,
						},
					},
				},
			},
			mockCloudReleaseError: true,
			expectErrorOnSync:     true,
		},
		{
			name: "Should fail to sync object on delete with wait error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the release and one for the wait
							// response
							ObservedGeneration: 4,
						},
					},
				},
			},
			mockCloudWaitError: true,
			expectErrorOnSync:  true,
		},
		{
			name: "Should be able to re-sync object on delete with no errors",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						// Fake an unsuccessful release in the last term
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionFalse,
							Reason:             cloudResponseReasonError,
							ObservedGeneration: 4,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name:       cloudPrivateIPConfigName,
					Finalizers: []string{},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionUnknown,
							Reason: cloudResponseReasonPending,
							// One update for the release
							ObservedGeneration: 5,
						},
					},
				},
			},
		},
		{
			name: "Should fail to re-sync object on delete with release error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						// Fake an unsuccessful release in the last term
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionFalse,
							Reason:             cloudResponseReasonError,
							ObservedGeneration: 4,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the release
							ObservedGeneration: 5,
						},
					},
				},
			},
			mockCloudReleaseError: true,
			expectErrorOnSync:     true,
		},
		{
			name: "Should fail to re-sync object on delete with wait error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					// Fake a deletion by setting the time to anything
					DeletionTimestamp: &v1.Time{time.Now()},
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						// Fake an unsuccessful release in the last term
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionFalse,
							Reason:             cloudResponseReasonError,
							ObservedGeneration: 4,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameA,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// One update for the release and one for the wait
							// response
							ObservedGeneration: 6,
						},
					},
				},
			},
			mockCloudWaitError: true,
			expectErrorOnSync:  true,
		},
	}
	runTests(t, tests)
}

func TestSyncUpdateCloudPrivateIPConfig(t *testing.T) {
	tests := []CloudPrivateIPConfigTestCase{
		{
			name: "Should be able to sync object on update without any errors",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameB,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionTrue,
							Reason: cloudResponseReasonSuccess,
							// four updates:
							// - release
							// - wait release
							// - assign
							// - wait assign
							ObservedGeneration: 6,
						},
					},
				},
			},
		},
		{
			name: "Should fail to sync object on update with release error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// one update:
							// - release
							ObservedGeneration: 3,
						},
					},
				},
			},
			mockCloudReleaseError: true,
			expectErrorOnSync:     true,
		},
		{
			name: "Should fail to sync object on update with wait on release error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// two updates:
							// - release
							// - wait release
							ObservedGeneration: 4,
						},
					},
				},
			},
			mockCloudWaitError: true,
			expectErrorOnSync:  true,
		},
		{
			name: "Should fail to sync object on update with assign error",
			testObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Node: nodeNameA,
					Conditions: []v1.Condition{
						v1.Condition{
							Type:               string(cloudnetworkv1.Assigned),
							Status:             v1.ConditionTrue,
							Reason:             cloudResponseReasonSuccess,
							ObservedGeneration: 2,
						},
					},
				},
			},
			expectedObject: &cloudnetworkv1.CloudPrivateIPConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: cloudPrivateIPConfigName,
					Finalizers: []string{
						cloudPrivateIPConfigFinalizer,
					},
				},
				Spec: cloudnetworkv1.CloudPrivateIPConfigSpec{
					Node: nodeNameB,
				},
				Status: cloudnetworkv1.CloudPrivateIPConfigStatus{
					Conditions: []v1.Condition{
						v1.Condition{
							Type:   string(cloudnetworkv1.Assigned),
							Status: v1.ConditionFalse,
							Reason: cloudResponseReasonError,
							// three updates:
							// - release
							// - wait release
							// - assign
							ObservedGeneration: 5,
						},
					},
				},
			},
			mockCloudAssignError: true,
			expectErrorOnSync:    true,
		},
	}
	runTests(t, tests)
}

func runTests(t *testing.T, tests []CloudPrivateIPConfigTestCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := tt.NewFakeCloudPrivateIPConfigController()
			if err := controller.SyncHandler(tt.testObject.Name); err != nil && !tt.expectErrorOnSync {
				t.Fatalf("sync expected no error, but got err: %v", err)
			}
			syncedObject, err := controller.cloudNetworkClient.CloudV1().CloudPrivateIPConfigs().Get(context.TODO(), tt.testObject.Name, v1.GetOptions{})
			if err != nil {
				t.Fatalf("could not get object for test assertion, err: %v", err)
			}
			if err := assertSyncedExpectedObjectsEqual(syncedObject, tt.expectedObject); err != nil {
				t.Fatalf("synced object did not match expected one, err: %v", err)
			}
		})
	}
}
