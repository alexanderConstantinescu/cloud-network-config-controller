package admissioncontroller

import (
	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	v1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
)

var (
	UniversalDeserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
)

type AdmissionControllerIntf interface {
	AdmissionFunc(*v1.AdmissionRequest) error
}

type AdmissionController struct {
	AdmissionControllerIntf
	CloudNetworkClient *cloudnetworkclientset.Clientset
	KubeClient         *kubernetes.Clientset
}
