package cloudprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	google "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
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

// GCPWaitInput is the required input for the GCP zone operations API call. All
// GCP infrastructure modifications are assigned a unique operation ID and are
// queued in a global/zone operations queue. In the case of assignments of
// private IP addresses to instances, the operation is added to the zone
// operations queue. Hence we need to keep the opName and the zone the instance
// lives in.
type GCPWaitInput struct {
	opName string
	zone   string
}

type secretData struct {
	ProjectID string `json:"project_id"`
}

func (g *GCP) initCredentials() (err error) {
	secretData := secretData{}
	rawSecretData, err := g.cloud.readSecretData("service_account.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(rawSecretData), &secretData); err != nil {
		return err
	}
	g.project = secretData.ProjectID
	g.client, err = google.NewService(context.TODO(), option.WithCredentialsFile(cloudProviderSecretLocation+"service_account.json"))
	if err != nil {
		return fmt.Errorf("error: cannot initialize google client, err: %v", err)
	}
	return nil
}

// AssignPrivateIP adds the IP to the associated instance's IP aliases.
// Important: GCP IP aliases can come in all forms, i.e: if you add 10.0.32.25
// GCP can return 10.0.32.25/32 or 10.0.32.25 - we thus need to check for both
// when validating that the IP provided doesn't already exist
func (g *GCP) AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	instance, err := g.getInstance(node)
	if err != nil {
		return nil, err
	}
	var opName string
	for _, networkInterface := range instance.NetworkInterfaces {
		for _, aliasIPRange := range networkInterface.AliasIpRanges {
			if assignedIP := net.ParseIP(aliasIPRange.IpCidrRange); ip != nil && assignedIP.Equal(ip) {
				return nil, AlreadyExistingIPError
			}
			if _, assignedSubnet, err := net.ParseCIDR(aliasIPRange.IpCidrRange); err == nil && assignedSubnet.Contains(ip) {
				return nil, AlreadyExistingIPError
			}
		}
		networkInterface.AliasIpRanges = append(networkInterface.AliasIpRanges, &google.AliasIpRange{
			IpCidrRange: ip.String(),
		})
		operation, err := g.client.Instances.UpdateNetworkInterface(g.project, g.parseZone(instance.Zone), instance.Name, networkInterface.Name, networkInterface).Do()
		if err != nil {
			return nil, err
		}
		opName = operation.Name
		break
	}
	return GCPWaitInput{
		opName: opName,
		zone:   g.parseZone(instance.Zone),
	}, nil
}

// ReleasePrivateIP removes the IP alias from the associated instance.
// Important: GCP IP aliases can come in all forms, i.e: if you add 10.0.32.25
// GCP can return 10.0.32.25/32 or 10.0.32.25
func (g *GCP) ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	instance, err := g.getInstance(node)
	if err != nil {
		return nil, err
	}
	var opName string
	for _, networkInterface := range instance.NetworkInterfaces {
		keepAliases := []*google.AliasIpRange{}
		for _, aliasIPRange := range networkInterface.AliasIpRanges {
			if assignedIP := net.ParseIP(aliasIPRange.IpCidrRange); ip != nil && assignedIP != nil && !assignedIP.Equal(ip) {
				keepAliases = append(keepAliases, aliasIPRange)
				continue
			}
			if assignedIP, _, err := net.ParseCIDR(aliasIPRange.IpCidrRange); err == nil && !assignedIP.Equal(ip) {
				keepAliases = append(keepAliases, aliasIPRange)
			}
		}
		networkInterface.AliasIpRanges = keepAliases
		operation, err := g.client.Instances.UpdateNetworkInterface(g.project, g.parseZone(instance.Zone), instance.Name, networkInterface.Name, networkInterface).Do()
		if err != nil {
			return nil, err
		}
		opName = operation.Name
		break
	}
	return GCPWaitInput{
		opName: opName,
		zone:   g.parseZone(instance.Zone),
	}, nil
}

func (g *GCP) WaitForResponse(requestObj interface{}) error {
	gcpWaitInput, ok := requestObj.(GCPWaitInput)
	if !ok {
		return fmt.Errorf("error decoding GCP requestObj, object not of type: GCPWaitInput %#v", requestObj)
	}
	_, err := g.client.ZoneOperations.Wait(g.project, gcpWaitInput.zone, gcpWaitInput.opName).Do()
	return err
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

// GCP Zone URLs are defined as:
// - https://www.googleapis.com/compute/v1/projects/openshift-gce-devel-ci/zones/us-east1-c
// OR
// - projects/project/zones/zone
func (g *GCP) parseZone(zoneURL string) string {
	zoneParts := strings.Split(zoneURL, "/")
	return zoneParts[len(zoneParts)-1]
}
