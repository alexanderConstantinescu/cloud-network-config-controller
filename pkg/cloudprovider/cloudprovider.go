package cloudprovider

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/fsnotify/fsnotify.v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	cloudProviderSecretLocation  = "/etc/secret/cloudprovider/"
	cloudProviderPollInterval    = 5
	cloudProviderTimeoutDuration = 60
)

type CloudProviderIntf interface {
	initCredentials() error
	watchForSecretChanges()
	// AssignPrivateIP attempts at assigning the IP address provided
	// to the VM instance corresponding to the corev1.Node provided
	// on the cloud the cluster is deployed on.
	// NOTE: this operation is only performed against the first
	// network interface defined for the VM.
	AssignPrivateIP(ip net.IP, node *corev1.Node) error
	// ReleasePrivateIP attempts at releasing the IP address provided
	// from the VM instance corresponding to the corev1.Node provided
	// on the cloud the cluster is deployed on.
	// NOTE: this operation is only performed against the first
	// network interface defined for the VM.
	ReleasePrivateIP(ip net.IP, node *corev1.Node) error
	// GetNodeSubnet attempts at retrieving the IPv4 and IPv6 subnets
	// from the VM instance corresponding to the corev1.Node provided
	// on the cloud the cluster is deployed on.
	// NOTE: this operation is only performed against the first
	// network interface defined for the VM.
	GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error)
}

type CloudProvider struct {
	intf CloudProviderIntf
}

func NewCloudProviderClient(cloudProvider string) (CloudProviderIntf, error) {
	var cloudProviderIntf CloudProviderIntf
	switch cloudProvider {
	case azure:
		{
			cloudProviderIntf = &Azure{}
		}
	case aws:
		{
			cloudProviderIntf = &AWS{}
		}
	case gcp:
		{
			cloudProviderIntf = &GCP{}
		}
	default:
		{
			return nil, fmt.Errorf("unsupported cloud provider: %s", cloudProvider)
		}
	}
	go cloudProviderIntf.watchForSecretChanges()
	return cloudProviderIntf, cloudProviderIntf.initCredentials()
}

func (c *CloudProvider) readSecretData(secret string) (string, error) {
	data, err := ioutil.ReadFile(cloudProviderSecretLocation + secret)
	if err != nil {
		return "", fmt.Errorf("unable to read secret data, err: %v", err)
	}
	return string(data), nil
}

func (c *CloudProvider) watchForSecretChanges() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		klog.Fatal(err)
	}
	defer watcher.Close()
	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					klog.Infof("Cloud provider secret data has been re-written, re-initializing credentials")
					if err := c.intf.initCredentials(); err != nil {
						klog.Errorf("Error re-initializing credentials, will send SIGTERM and shutdown, err: %v", err)
						// Don't do os.Exit here, instead trigger a SIGTERM which will call our
						// signal handlers and initiate a controlled shutdown of all controllers
						syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	// Secrets are symlinks, where one synlink is created for each `.data`
	// attribute in the secret. Since `.data` depends on the cloud provider,
	// watch as many as 10. If any one of these gets re-written, we kill ourselves.
	p := cloudProviderSecretLocation
	maxdepth := 10
	for depth := 0; depth < maxdepth; depth++ {
		if err := watcher.Add(p); err != nil {
			klog.Fatal(err)
		}
		stat, err := os.Lstat(p)
		if err != nil {
			klog.Fatal(err)
		}
		if stat.Mode()&os.ModeSymlink > 0 {
			p, err = filepath.EvalSymlinks(p)
			if err != nil {
				klog.Fatal(err)
			}
		} else {
			break
		}
	}
	<-done
}

func parseProviderID(providerID string) []string {
	return strings.Split(providerID, "/")
}
