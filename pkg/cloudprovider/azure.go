package cloudprovider

import (
	"context"
	"fmt"
	"net"
	"strings"

	network "github.com/Azure/azure-sdk-for-go/profiles/latest/network/mgmt/network"
	compute "github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-30/compute"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	azureapi "github.com/Azure/go-autorest/autorest/azure"
	corev1 "k8s.io/api/core/v1"
	utilnet "k8s.io/utils/net"
)

const (
	azure = "azure"
)

// Azure implements the API wrapper for talking
// to the Azure cloud API
type Azure struct {
	cloud         CloudProvider
	resourceGroup string
	vmClient      compute.VirtualMachinesClient
	networkClient network.InterfacesClient
}

func (a *Azure) initCredentials() error {
	clientID, err := a.readSecretData("azure_client_id")
	if err != nil {
		return err
	}
	tenantID, err := a.readSecretData("azure_tenant_id")
	if err != nil {
		return err
	}
	clientSecret, err := a.readSecretData("azure_client_secret")
	if err != nil {
		return err
	}
	subscriptionID, err := a.readSecretData("azure_subscription_id")
	if err != nil {
		return err
	}
	a.resourceGroup, err = a.readSecretData("azure_resourcegroup")
	if err != nil {
		return err
	}
	authorizer, err := a.getAuthorizer(clientID, clientSecret, tenantID)
	if err != nil {
		return err
	}

	a.vmClient = compute.NewVirtualMachinesClient(subscriptionID)
	a.vmClient.Authorizer = authorizer
	a.vmClient.AddToUserAgent(azure)

	a.networkClient = network.NewInterfacesClient(subscriptionID)
	a.networkClient.Authorizer = authorizer
	a.networkClient.AddToUserAgent(azure)
	return nil
}

func (a *Azure) AssignPrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := a.getInstance(node)
	if err != nil {
		return err
	}
	networkInterface := network.Interface{}
	for _, netif := range *instance.NetworkProfile.NetworkInterfaces {
		if *netif.Primary {
			var err error
			networkInterface, err = a.networkClient.Get(context.TODO(), a.resourceGroup, getNameFromResourceID(*netif.ID), "")
			if err != nil {
				return err
			}
			for _, ipConfiguration := range *networkInterface.IPConfigurations {
				if assignedIP := net.ParseIP(*ipConfiguration.PrivateIPAddress); assignedIP != nil && assignedIP.Equal(ip) {
					return nil
				}
			}
			break
		}
	}
	ipConfigurations := *networkInterface.IPConfigurations
	name := fmt.Sprintf("%s_%s", node, ip.String())
	ipc := ip.String()
	// Damn you Azure for making me do things like this!
	untrue := false
	newIPConfiguration := network.InterfaceIPConfiguration{
		Name: &name,
		InterfaceIPConfigurationPropertiesFormat: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress:                &ipc,
			PrivateIPAllocationMethod:       network.Static,
			Subnet:                          (*networkInterface.IPConfigurations)[0].Subnet,
			Primary:                         &untrue,
			LoadBalancerBackendAddressPools: (*networkInterface.IPConfigurations)[0].LoadBalancerBackendAddressPools,
		},
	}
	ipConfigurations = append(ipConfigurations, newIPConfiguration)
	networkInterface.IPConfigurations = &ipConfigurations
	result, err := a.networkClient.CreateOrUpdate(context.TODO(), a.resourceGroup, *networkInterface.Name, networkInterface)
	if err != nil {
		return err
	}
	return result.WaitForCompletionRef(context.TODO(), a.networkClient.Client)
}

func (a *Azure) ReleasePrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := a.getInstance(node)
	if err != nil {
		return err
	}
	networkInterface := network.Interface{}
	keepIPConfiguration := []network.InterfaceIPConfiguration{}
	for _, netif := range *instance.NetworkProfile.NetworkInterfaces {
		if *netif.Primary {
			var err error
			networkInterface, err = a.networkClient.Get(context.TODO(), a.resourceGroup, getNameFromResourceID(*netif.ID), "")
			if err != nil {
				return err
			}
			for _, ipConfiguration := range *networkInterface.IPConfigurations {
				if assignedIP := net.IP(*ipConfiguration.PrivateIPAddress); assignedIP != nil && !assignedIP.Equal(ip) {
					keepIPConfiguration = append(keepIPConfiguration, ipConfiguration)
				}
			}
			break
		}
	}
	networkInterface.IPConfigurations = &keepIPConfiguration
	result, err := a.networkClient.CreateOrUpdate(context.TODO(), a.resourceGroup, *networkInterface.Name, networkInterface)
	if err != nil {
		return err
	}
	return result.WaitForCompletionRef(context.TODO(), a.networkClient.Client)
}

func (a *Azure) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	instance, err := a.getInstance(node)
	if err != nil {
		return nil, nil, err
	}
	var primaryNicID string
	for _, networkInterface := range *instance.NetworkProfile.NetworkInterfaces {
		if *networkInterface.Primary {
			primaryNicID = *networkInterface.ID
			break
		}
	}
	addressPrefixes, err := a.getAddressPrefixes(primaryNicID)
	if err != nil {
		return nil, nil, err
	}
	var v4Subnet, v6Subnet *net.IPNet
	for _, addressPrefix := range addressPrefixes {
		_, subnet, err := net.ParseCIDR(addressPrefix)
		if err != nil {
			return nil, nil, fmt.Errorf("error: unable to parse found AddressPrefix: %s, err: %v", addressPrefix, err)
		}
		if utilnet.IsIPv6CIDR(subnet) {
			v6Subnet = subnet
		} else {
			v4Subnet = subnet
		}
	}
	return v4Subnet, v6Subnet, nil
}

//  This is what the node's providerID looks like on Azure
// 	spec:
//   providerID: azure:///subscriptions/ee2e2172-e246-4d4b-a72a-f62fbf924238/resourceGroups/ovn-qgwkn-rg/providers/Microsoft.Compute/virtualMachines/ovn-qgwkn-worker-canadacentral1-bskbf
func (a *Azure) getInstance(node *corev1.Node) (*compute.VirtualMachine, error) {
	providerData := parseProviderID(node.Spec.ProviderID)
	instance, err := a.vmClient.Get(context.TODO(), a.resourceGroup, providerData[len(providerData)-1], "")
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

func (a *Azure) getAddressPrefixes(nicID string) ([]string, error) {
	networkInterface, err := a.networkClient.Get(context.TODO(), a.resourceGroup, getNameFromResourceID(nicID), "")
	if err != nil {
		return nil, err
	}
	for _, ipConfiguration := range *networkInterface.IPConfigurations {
		if *ipConfiguration.Primary {
			if ipConfiguration.Subnet.AddressPrefixes != nil {
				return *ipConfiguration.Subnet.AddressPrefixes, nil
			}
			if *ipConfiguration.Subnet.AddressPrefix != "" {
				return []string{*ipConfiguration.Subnet.AddressPrefix}, nil
			}
			break
		}
	}
	return nil, fmt.Errorf("error: unable to find subnet prefixes for primary IP address")
}

func (a *Azure) watchForSecretChanges() {
	a.cloud.watchForSecretChanges()
}

func (a *Azure) readSecretData(secretName string) (string, error) {
	return a.cloud.readSecretData(secretName)
}

func (a *Azure) getAuthorizer(clientID string, clientSecret string, tenantID string) (autorest.Authorizer, error) {
	oauthConfig, err := adal.NewOAuthConfig(azureapi.PublicCloud.ActiveDirectoryEndpoint, tenantID)
	if err != nil {
		return nil, err
	}
	spToken, err := adal.NewServicePrincipalToken(*oauthConfig, clientID, clientSecret, azureapi.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return nil, err
	}
	return autorest.NewBearerAuthorizer(spToken), nil
}

func getNameFromResourceID(id string) string {
	return id[strings.LastIndex(id, "/"):]
}
