package cloudprovider

import (
	"net"

	corev1 "k8s.io/api/core/v1"
)

const (
	azure = "azure"
)

// Azure implements the API wrapper for talking
// to the Azure cloud API
type Azure struct {
	cloud CloudProvider
}

func (a *Azure) initCredentials() error {
	return nil
}

func (a *Azure) AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	return nil, nil
}

func (a *Azure) ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	return nil, nil
}

func (a *Azure) WaitForResponse(interface{}) error {
	return nil
}

func (a *Azure) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}
