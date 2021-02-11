package cloudprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	google "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	gcp = "gcp"
)

// GCP implements the API wrapper for talking
// to the GCP cloud API
type GCP struct {
	cloud   CloudProvider
	client  *google.Service
	project string
}

type secretData struct {
	ProjectID string `json:"project_id"`
}

func (g *GCP) initCredentials() (err error) {
	secretData := secretData{}
	rawSecretData, err := g.cloud.readSecretData(cloudProviderSecretLocation + "service_account.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(rawSecretData), secretData); err != nil {
		return err
	}
	g.project = secretData.ProjectID
	g.client, err = google.NewService(context.TODO(), option.WithCredentialsFile(cloudProviderSecretLocation+"service_account.json"))
	if err != nil {
		return fmt.Errorf("error: cannot initialize google client, err: %v", err)
	}
	return nil
}

func (g *GCP) AssignPrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := g.getInstance(node)
	if err != nil {
		return err
	}
	var opName string
	for _, networkInterface := range instance.NetworkInterfaces {
		for _, aliasIPRange := range networkInterface.AliasIpRanges {
			if assignedIP := net.ParseIP(aliasIPRange.IpCidrRange); ip != nil && assignedIP.Equal(ip) {
				return fmt.Errorf("error: cannot assign already existing IP alias: %s", assignedIP.String())
			}
			if _, assignedSubnet, err := net.ParseCIDR(aliasIPRange.IpCidrRange); err == nil && assignedSubnet.Contains(ip) {
				return fmt.Errorf("error: cannot assign IP: %s, IP subnet alias: %s includes the IP already", ip.String(), assignedSubnet.String())
			}
		}
		networkInterface.AliasIpRanges = append(networkInterface.AliasIpRanges, &google.AliasIpRange{
			IpCidrRange: ip.String(),
		})
		operation, err := g.client.Instances.UpdateNetworkInterface(g.project, instance.Zone, instance.Name, networkInterface.Name, networkInterface).Do()
		if err != nil {
			return err
		}
		opName = operation.Name
		break
	}
	return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
		operation, err := g.client.ZoneOperations.Get(g.project, instance.Zone, opName).Do()
		return operation.Status == "DONE", err
	})
}

func (g *GCP) ReleasePrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := g.getInstance(node)
	if err != nil {
		return err
	}
	var opName string
	for _, networkInterface := range instance.NetworkInterfaces {
		keepAliases := []*google.AliasIpRange{}
		for _, aliasIPRange := range networkInterface.AliasIpRanges {
			if assignedIP := net.ParseIP(aliasIPRange.IpCidrRange); ip != nil && !assignedIP.Equal(ip) {
				keepAliases = append(keepAliases, aliasIPRange)
			}
			if assignedIP, _, err := net.ParseCIDR(aliasIPRange.IpCidrRange); err == nil && !assignedIP.Equal(ip) {
				keepAliases = append(keepAliases, aliasIPRange)
			}
		}
		networkInterface.AliasIpRanges = keepAliases
		operation, err := g.client.Instances.UpdateNetworkInterface(g.project, instance.Zone, instance.Name, networkInterface.Name, networkInterface).Do()
		if err != nil {
			return err
		}
		opName = operation.Name
		break
	}
	return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
		operation, err := g.client.ZoneOperations.Get(g.project, instance.Zone, opName).Do()
		return operation.Status == "DONE", err
	})
}

func (g *GCP) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	instance, err := g.getInstance(node)
	if err != nil {
		return nil, nil, err
	}
	var v4Subnet, v6Subnet *net.IPNet
	for _, networkInterface := range instance.NetworkInterfaces {
		region, subnet := g.parseSubnet(networkInterface.Subnetwork)
		subnetResult, err := g.client.Subnetworks.Get(g.project, region, subnet).Do()
		if err != nil {
			return nil, nil, err
		}
		if subnetResult.IpCidrRange != "" {
			_, v4Subnet, _ = net.ParseCIDR(subnetResult.IpCidrRange)
		}
		if subnetResult.Ipv6CidrRange != "" {
			_, v6Subnet, _ = net.ParseCIDR(subnetResult.Ipv6CidrRange)
		}
		break
	}
	return v4Subnet, v6Subnet, nil
}

//  This is what the node's providerID looks like on GCP
// 	spec:
//   providerID: gce://openshift-gce-devel-ci/us-east1-b/ci-ln-pvr3lyb-f76d1-6w8mm-master-0
//  i.e: projectID/zone/instanceName
func (g *GCP) getInstance(node *corev1.Node) (*google.Instance, error) {
	providerData := parseProviderID(node.Spec.ProviderID)
	instance, err := g.client.Instances.Get(providerData[len(providerData)-3], providerData[len(providerData)-2], providerData[len(providerData)-1]).Do()
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// GCP Subnet URLs are defined as:
// - https://www.googleapis.com/compute/v1/projects/project/regions/region/subnetworks/subnetwork
// OR
// - regions/region/subnetworks/subnetwork
func (g *GCP) parseSubnet(subnetURL string) (string, string) {
	subnetURLParts := strings.Split(subnetURL, "/")
	return subnetURLParts[len(subnetURLParts)-3], subnetURLParts[len(subnetURLParts)-1]
}

func (g *GCP) watchForSecretChanges() {
	g.cloud.watchForSecretChanges()
}
