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

func (a *AWS) InitCredentials() error {
	return nil
}

func (a *AWS) AssignPrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *AWS) ReleasePrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *AWS) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}

func (a *AWS) WatchForSecretChanges() {
	a.cloud.WatchForSecretChanges()
}
