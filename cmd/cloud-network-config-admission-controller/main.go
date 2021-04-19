package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	admissioncontroller "github.com/openshift/cloud-network-config-controller/pkg/admissioncontroller"
	cloudprivateipconfigadmissioncontroller "github.com/openshift/cloud-network-config-controller/pkg/admissioncontroller/cloudprivateipconfig"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	tlsDir          = `/run/secrets/tls`
	tlsCertFile     = `tls.crt`
	tlsKeyFile      = `tls.key`
	jsonContentType = `application/json`
)

var (
	masterURL  string
	kubeconfig string
)

type AdmissionControllerIntf interface {
	AdmissionFunc(*v1.AdmissionRequest) error
}

type AdmissionController struct {
	AdmissionControllerIntf
	cloudNetworkClient *cloudnetworkclientset.Clientset
	kubeClient         *kubernetes.Clientset
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}

func main() {

	klog.InitFlags(nil)
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}
	cloudNetworkClient, err := cloudnetworkclientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building cloudnetwork clientset: %s", err.Error())
	}
	admissionController := cloudprivateipconfigadmissioncontroller.NewCloudPrivateIPConfigAdmissionController(cloudNetworkClient, kubeClient)

	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)

	mux := http.NewServeMux()
	mux.Handle("/"+cloudprivateipconfigadmissioncontroller.CloudPrivateIPConfigResource.Resource, admitFuncHandler(admissionController))
	server := &http.Server{
		Addr:    ":8443",
		Handler: mux,
	}

	klog.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}

func doServeAdmitFunc(w http.ResponseWriter, r *http.Request, admit AdmissionControllerIntf) ([]byte, error) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("invalid method %s, only POST requests are allowed", r.Method)
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("could not read request body: %v", err)
	}

	if contentType := r.Header.Get("Content-Type"); contentType != jsonContentType {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("unsupported content type %s, only %s is supported", contentType, jsonContentType)
	}

	var admissionReviewReq v1.AdmissionReview

	if _, _, err := admissioncontroller.UniversalDeserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("could not deserialize request: %v", err)
	} else if admissionReviewReq.Request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, errors.New("malformed admission review: request is nil")
	}

	admissionReviewResponse := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "AdmissionReview",
		},
		Response: &v1.AdmissionResponse{
			UID: admissionReviewReq.Request.UID,
		},
	}

	err = admit.AdmissionFunc(admissionReviewReq.Request)

	if err != nil {
		admissionReviewResponse.Response.Allowed = false
		admissionReviewResponse.Response.Result = &metav1.Status{
			Message: err.Error(),
		}
	} else {
		admissionReviewResponse.Response.Allowed = true
	}

	bytes, err := json.Marshal(&admissionReviewResponse)
	if err != nil {
		return nil, fmt.Errorf("marshaling response: %v", err)
	}
	return bytes, nil
}

func serveAdmitFunc(w http.ResponseWriter, r *http.Request, admit AdmissionControllerIntf) {
	klog.Info("Handling webhook request")
	var writeErr error
	if bytes, err := doServeAdmitFunc(w, r, admit); err != nil {
		klog.Errorf("Error handling webhook request: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, writeErr = w.Write([]byte(err.Error()))
	} else {
		klog.Info("Webhook request handled successfully")
		_, writeErr = w.Write(bytes)
	}
	if writeErr != nil {
		klog.Errorf("Could not write response: %v", writeErr)
	}
}

func admitFuncHandler(admit AdmissionControllerIntf) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAdmitFunc(w, r, admit)
	})
}
