package main

import (
	"flag"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	cloudprivateipconfigcontroller "github.com/openshift/cloud-network-config-controller/pkg/controllers/cloudprivateipconfig"
	nodecontroller "github.com/openshift/cloud-network-config-controller/pkg/controllers/node"
	cloudprivateipconfigclientset "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/clientset/versioned"
	cloudprivateipconfiginformers "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1/apis/informers/externalversions"

	signals "github.com/openshift/cloud-network-config-controller/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	cloudPrivateIPConfigClient, err := cloudprivateipconfigclientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}

	cloudProviderClient, err := cloudprovider.NewCloudProviderClient()
	if err != nil {
		klog.Fatal("Error creating new cloud provider client, err: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	cloudPrivateIPInformerFactory := cloudprivateipconfiginformers.NewSharedInformerFactory(cloudPrivateIPConfigClient, time.Second*30)

	cloudPrivateIPConfigController := cloudprivateipconfigcontroller.NewCloudPrivateIPConfigController(
		kubeClient,
		cloudProviderClient,
		cloudPrivateIPConfigClient,
		cloudPrivateIPInformerFactory.Network().V1().CloudPrivateIPConfigs(),
		kubeInformerFactory.Core().V1().Nodes(),
	)
	nodeController := nodecontroller.NewNodeController(
		kubeClient,
		cloudProviderClient,
		kubeInformerFactory.Core().V1().Nodes(),
	)

	cloudPrivateIPInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)

	if err = cloudPrivateIPConfigController.Run(1, stopCh); err != nil {
		klog.Fatalf("Error running CloudPrivateIPConfig controller: %s", err.Error())
	}
	if err = nodeController.Run(1, stopCh); err != nil {
		klog.Fatalf("Error running Node controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}
