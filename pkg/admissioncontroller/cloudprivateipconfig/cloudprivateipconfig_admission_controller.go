package admissioncontroller

import (
	"context"
	"fmt"

	cloudnetworkv1 "github.com/openshift/api/cloudnetwork/v1"
	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	"github.com/openshift/cloud-network-config-controller/pkg/admissioncontroller"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	CloudPrivateIPConfigResource = metav1.GroupVersionResource{
		Version:  cloudnetworkv1.SchemeGroupVersion.Version,
		Group:    cloudnetworkv1.SchemeGroupVersion.Group,
		Resource: "cloudprivateipconfigs",
	}
)

type CloudPrivateIPConfigAdmissionController struct {
	admissioncontroller.AdmissionController
}

func NewCloudPrivateIPConfigAdmissionController(
	cloudNetworkClient *cloudnetworkclientset.Clientset,
	kubeClient *kubernetes.Clientset) *CloudPrivateIPConfigAdmissionController {
	return &CloudPrivateIPConfigAdmissionController{
		AdmissionController: admissioncontroller.AdmissionController{
			CloudNetworkClient: cloudNetworkClient,
			KubeClient:         kubeClient,
		},
	}
}

func (c *CloudPrivateIPConfigAdmissionController) AdmissionFunc(req *admissionv1.AdmissionRequest) error {
	if req.Resource != CloudPrivateIPConfigResource {
		return fmt.Errorf("expect resource to be %s, got: %s", CloudPrivateIPConfigResource, &req.Resource)
	}
	raw := req.Object.Raw
	cloudPrivateIPConfig := &cloudnetworkv1.CloudPrivateIPConfig{}
	if _, _, err := admissioncontroller.UniversalDeserializer.Decode(raw, nil, cloudPrivateIPConfig); err != nil {
		return fmt.Errorf("error processing admission for CloudPrivateIPConfig: %s, unable to deserialize CloudPrivateIPConfig object: %v", cloudPrivateIPConfig.Name, err)
	}
	_, err := c.KubeClient.CoreV1().Nodes().Get(context.Background(), cloudPrivateIPConfig.Spec.Node, metav1.GetOptions{})
	return err
}
