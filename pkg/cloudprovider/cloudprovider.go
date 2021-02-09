package cloudprovider

import (
	"net"

	corev1 "k8s.io/api/core/v1"
)

type CloudProviderIntf interface {
	AssignPrivateIP(ip net.IP, node string) error
	ReleasePrivateIP(ip net.IP, node string) error
	GetNodeSubnet(node *corev1.Node) error
}

type CloudProvider struct {
	intf CloudProviderIntf
}

func NewCloudProviderClient() (*CloudProvider, error) {
	return &CloudProvider{}, nil
}

func (*CloudProvider) AssignPrivateIP(ip net.IP, node string) error {
	return nil
}

func (*CloudProvider) ReleasePrivateIP(ip net.IP, node string) error {
	return nil
}

func (*CloudProvider) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}
