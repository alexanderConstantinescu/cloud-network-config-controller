package main

import (
	"context"
	"flag"
	"os"
	"sync"
	"time"

	cloudnetworkclientset "github.com/openshift/client-go/cloudnetwork/clientset/versioned"
	cloudnetworkinformers "github.com/openshift/client-go/cloudnetwork/informers/externalversions"
	cloudprovider "github.com/openshift/cloud-network-config-controller/pkg/cloudprovider"
	cloudprivateipconfigcontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/cloudprivateipconfig"
	nodecontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/node"
	secretcontroller "github.com/openshift/cloud-network-config-controller/pkg/controller/secret"
	signals "github.com/openshift/cloud-network-config-controller/pkg/signals"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

const (
	// The name of the configmap used for leader election
	resourceLockName = "cloud-network-config-controller-lock"
)

var (
	masterURL       string
	kubeconfig      string
	cloudProvider   string
	cloudRegion     string
	secretName      string
	secretNamespace string
	podName         string
	podNamespace    string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up wait group used for spawning all our individual controllers
	// on the bottom of this function
	wg := &sync.WaitGroup{}

	// set up a global context used for shutting down the leader election and
	// subsequently all controllers.
	ctx, cancelFunc := context.WithCancel(context.Background())

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler(cancelFunc)

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	// set up leader election, the only reason for this is to make sure we only
	// have one replica of this controller at any given moment in time. On
	// upgrades there could be small windows where one replica of the deployment
	// stops on one node while another starts on another. In such a case we
	// could have both running at the same time. This prevents that from
	// happening and ensures we only have one replica "controlling", always.
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.ConfigMapLock{
			ConfigMapMeta: metav1.ObjectMeta{
				Name:      resourceLockName,
				Namespace: podNamespace,
			},
			Client: kubeClient.CoreV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: podName,
			},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {

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
					cancelFunc,
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
			},
			// There are two cases to consider for shutting down our controller.
			//  1. Cloud credential rotation - which our secret controller
			//     watches for and cancels the global context. That will trigger
			//     an end to the leader election loop and call OnStoppedLeading
			//     which will send a SIGTERM and shut down all controllers.
			//  2. Leader election rotation - which will send a SIGTERM and
			//     shut down all controllers.
			OnStoppedLeading: func() {
				klog.Info("Stopped leading, sending SIGTERM and shutting down controller")
				signals.ShutDown()
				// Only wait if we were ever leader.
				wg.Wait()
			},
		},
	})
	klog.Info("Finished executing controlled shutdown")
}

func init() {
	// These are arguments for this controller
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&cloudProvider, "cloudprovider", "", "The cloud provider this component is running on.")
	flag.StringVar(&cloudRegion, "cloudregion", "", "The cloud region the cluster is deployed in, this is explicitly required for talking to the AWS API.")
	flag.StringVar(&secretName, "secret-name", "", "The cloud provider secret name - used for talking to the cloud API.")
	flag.StringVar(&secretNamespace, "secret-namespace", "", "The cloud provider secret namespace - used for talking to the cloud API.")

	// These are populate by the downward API
	podNamespace = os.Getenv("POD_NAMESPACE")
	podName = os.Getenv("POD_NAME")
}
