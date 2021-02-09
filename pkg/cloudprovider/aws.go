package cloudprovider

import (
	"net"

	corev1 "k8s.io/api/core/v1"
)

const (
	aws = "aws"
)

// AWS implements the API wrapper for talking
// to the AWS cloud API
type AWS struct {
	cloud CloudProvider
}

func (a *AWS) initCredentials() error {
	return nil
}

func (a *AWS) AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	return nil, nil
}

func (a *AWS) ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	return nil, nil
}

func (a *AWS) WaitForResponse(interface{}) error {
	return nil
}

func (a *AWS) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}
