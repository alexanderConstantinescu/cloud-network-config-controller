package cloudprovider

import (
	"net"

	network "github.com/Azure/azure-sdk-for-go/profiles/latest/network/mgmt/network"
	compute "github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-30/compute"
	corev1 "k8s.io/api/core/v1"
)

const (
	azure = "azure"
)

// Azure implements the API wrapper for talking
// to the Azure cloud API
type Azure struct {
	cloud         CloudProvider
	vmClient      compute.VirtualMachinesClient
	vnetClient    network.VirtualNetworksClient
	networkClient network.InterfacesClient
}

func (a *Azure) InitCredentials() error {
	return nil
}

func (a *Azure) AssignPrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *Azure) ReleasePrivateIP(ip net.IP, node string) error {
	return nil
}

func (a *Azure) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	return nil, nil, nil
}

func (a *Azure) WatchForSecretChanges() {
	a.cloud.WatchForSecretChanges()
}
