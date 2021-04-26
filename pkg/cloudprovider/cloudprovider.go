package cloudprovider

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	cloudProviderSecretLocation = "/etc/secret/cloudprovider/"
)

var AlreadyExistingIPError = errors.New("the requested IP is already assigned")

type CloudProviderIntf interface {
	initCredentials() error
	// AssignPrivateIP attempts at assigning the IP address provided to the VM
	// instance corresponding to the corev1.Node provided on the cloud the
	// cluster is deployed on. NOTE: this operation is only performed against
	// the first network interface defined for the VM. It will return an
	// AlreadyExistingIPError if the IP provided is already associated with the
	// node, it's up to the caller to decided what to do with that.
	AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error)
	// ReleasePrivateIP attempts at releasing the IP address provided from the
	// VM instance corresponding to the corev1.Node provided on the cloud the
	// cluster is deployed on. NOTE: this operation is only performed against
	// the first network interface defined for the VM.
	ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error)
	// WaitForResponse runs a long function running call waiting for the cloud's
	// response to the previously called Assign/ReleasePrivateIP. If it timeouts
	// or encounters an error, that error is then returned. The function
	// argument accepts whatever resource needed by the cloud to determine the
	// status of the in-flight request.
	WaitForResponse(interface{}) error
	// GetNodeSubnet attempts at retrieving the IPv4 and IPv6 subnets from the
	// VM instance corresponding to the corev1.Node provided on the cloud the
	// cluster is deployed on. NOTE: this operation is only performed against
	// the first network interface defined for the VM.
	GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error)
}

type CloudProvider struct {
	intf CloudProviderIntf
}

func NewCloudProviderClient(cloudProvider, cloudRegion string) (CloudProviderIntf, error) {
	var cloudProviderIntf CloudProviderIntf
	switch strings.ToLower(cloudProvider) {
	case azure:
		{
			cloudProviderIntf = &Azure{}
		}
	case aws:
		{
			cloudProviderIntf = &AWS{region: cloudRegion}
		}
	case gcp:
		{
			cloudProviderIntf = &GCP{}
		}
	default:
		{
			return nil, fmt.Errorf("unsupported cloud provider: %s", strings.ToLower(cloudProvider))
		}
	}
	return cloudProviderIntf, cloudProviderIntf.initCredentials()
}

func (c *CloudProvider) readSecretData(secret string) (string, error) {
	data, err := ioutil.ReadFile(cloudProviderSecretLocation + secret)
	if err != nil {
		return "", fmt.Errorf("unable to read secret data, err: %v", err)
	}
	return string(data), nil
}

func parseProviderID(providerID string) []string {
	return strings.Split(providerID, "/")
}
