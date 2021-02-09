package cloudprovider

import (
	"net"

	corev1 "k8s.io/api/core/v1"
)

const (
	gcp = "gcp"
)

// GCP implements the API wrapper for talking
// to the GCP cloud API
type GCP struct {
	cloud CloudProvider
}

func (a *GCP) InitCredentials() error {
	return nil
}

func (a *GCP) AssignPrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *GCP) ReleasePrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *GCP) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}

func (a *GCP) WatchForSecretChanges() {
	a.cloud.WatchForSecretChanges()
}
