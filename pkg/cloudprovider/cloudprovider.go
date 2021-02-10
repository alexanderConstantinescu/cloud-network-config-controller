package cloudprovider

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/fsnotify/fsnotify.v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	cloudProviderSecretLocation = "/etc/secret/cloudprovider/"
)

type CloudProviderIntf interface {
	InitCredentials() error
	WatchForSecretChanges()
	AssignPrivateIP(ip net.IP, node string) error
	ReleasePrivateIP(ip net.IP, node string) error
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
	go cloudProviderIntf.WatchForSecretChanges()
	return cloudProviderIntf, cloudProviderIntf.InitCredentials()
}

func (c *CloudProvider) WatchForSecretChanges() {
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
					if err := c.intf.InitCredentials(); err != nil {
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
