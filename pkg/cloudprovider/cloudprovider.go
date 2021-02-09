package cloudprovider

import (
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
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
}
