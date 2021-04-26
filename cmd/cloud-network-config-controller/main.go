package main

import (
	"flag"
	"sync"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	cloudprivateipconfigcontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/cloudprivateipconfig"
	nodecontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/node"
	secretcontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/secret"
	signals "github.com/openshift/cloud-network-config-controller/pkg/signals"
)

var (
	masterURL       string
	kubeconfig      string
	cloudProvider   string
	cloudRegion     string
	secretName      string
	secretNamespace string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up wait group used for spawning all our individual controllers
	// on the bottom of this function
	wg := &sync.WaitGroup{}

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

	cloudNetworkClient, err := cloudnetworkclientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building cloudnetwork clientset: %s", err.Error())
	}

	cloudProviderClient, err := cloudprovider.NewCloudProviderClient(cloudProvider, cloudRegion)
	if err != nil {
		klog.Fatal("Error building cloud provider client, err: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	cloudNetworkInformerFactory := cloudnetworkinformers.NewSharedInformerFactory(cloudNetworkClient, time.Second*30)

	cloudPrivateIPConfigController := cloudprivateipconfigcontroller.NewCloudPrivateIPConfigController(
		cloudProviderClient,
		cloudNetworkClient,
		cloudNetworkInformerFactory.Cloud().V1().CloudPrivateIPConfigs(),
		kubeInformerFactory.Core().V1().Nodes(),
	)
	nodeController := nodecontroller.NewNodeController(
		kubeClient,
		cloudProviderClient,
		kubeInformerFactory.Core().V1().Nodes(),
	)
	secretController := secretcontroller.NewSecretController(
		kubeClient,
		kubeInformerFactory.Core().V1().Secrets(),
		secretName,
		secretNamespace,
	)

	cloudNetworkInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err = secretController.Run(stopCh); err != nil {
			klog.Fatalf("Error running Secret controller: %s", err.Error())
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err = cloudPrivateIPConfigController.Run(stopCh); err != nil {
			klog.Fatalf("Error running CloudPrivateIPConfig controller: %s", err.Error())
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err = nodeController.Run(stopCh); err != nil {
			klog.Fatalf("Error running Node controller: %s", err.Error())
		}
	}()
	wg.Wait()
	klog.Info("Finished executing controlled shutdown")
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&cloudProvider, "cloudprovider", "", "The cloud provider this component is running on.")
	flag.StringVar(&cloudRegion, "cloudregion", "", "The cloud region the cluster is deployed in, this is explicitly required for talking to the AWS API.")
	flag.StringVar(&secretName, "secret-name", "", "The cloud provider secret name - used for talking to the cloud API.")
	flag.StringVar(&secretNamespace, "secret-namespace", "", "The cloud provider secret namespace - used for talking to the cloud API.")
}
