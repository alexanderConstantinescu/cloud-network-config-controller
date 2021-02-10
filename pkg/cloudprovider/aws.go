package cloudprovider

import (
	"fmt"
	"net"
	"time"

	awsapi "github.com/aws/aws-sdk-go/aws"
	awscredentials "github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	utilnet "k8s.io/utils/net"
)

const (
	aws = "aws"
)

// AWS implements the API wrapper for talking
// to the AWS cloud API
type AWS struct {
	cloud  CloudProvider
	client *ec2.EC2
}

func (a *AWS) initCredentials() error {
	accessKey, err := a.cloud.readSecretData("aws_access_key_id")
	if err != nil {
		return err
	}
	secretKey, err := a.cloud.readSecretData("aws_secret_access_key")
	if err != nil {
		return err
	}
	mySession := session.Must(session.NewSession())
	a.client = ec2.New(mySession, awsapi.NewConfig().WithCredentials(awscredentials.NewStaticCredentials(accessKey, secretKey, "")))
	return nil
}

func (a *AWS) AssignPrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := a.getInstance(node)
	if err != nil {
		return err
	}
	addIP := ip.String()
	if utilnet.IsIPv6(ip) {
		keepIPs := []*string{}
		for _, assignedIPv6 := range instance.NetworkInterfaces[0].Ipv6Addresses {
			if assignedIP := net.ParseIP(*assignedIPv6.Ipv6Address); assignedIP != nil && assignedIP.Equal(ip) {
				return fmt.Errorf("error: cannot assign existing IPv6: %v address to node: %s", ip, node)
			}
			keepIPs = append(keepIPs, assignedIPv6.Ipv6Address)
		}
		keepIPs = append(keepIPs, &addIP)
		input := ec2.AssignIpv6AddressesInput{
			NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
			Ipv6Addresses:      keepIPs,
		}
		_, err = a.client.AssignIpv6Addresses(&input)
		if err != nil {
			return err
		}
		return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
			instance, err := a.getInstance(node)
			return *instance.MetadataOptions.State == "applied", err
		})
	}
	keepIPs := []*string{}
	for _, assignedIPv4 := range instance.NetworkInterfaces[0].PrivateIpAddresses {
		if assignedIP := net.ParseIP(*assignedIPv4.PrivateIpAddress); assignedIP != nil && assignedIP.Equal(ip) {
			return fmt.Errorf("error: cannot assign existing IPv4: %v address to node: %s", ip, node)
		}
		keepIPs = append(keepIPs, assignedIPv4.PrivateIpAddress)
	}
	keepIPs = append(keepIPs, &addIP)
	inputV4 := ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
		PrivateIpAddresses: keepIPs,
	}
	_, err = a.client.AssignPrivateIpAddresses(&inputV4)
	if err != nil {
		return err
	}
	return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
		instance, err := a.getInstance(node)
		return *instance.MetadataOptions.State == "applied", err
	})
}

func (a *AWS) ReleasePrivateIP(ip net.IP, node *corev1.Node) error {
	instance, err := a.getInstance(node)
	if err != nil {
		return err
	}
	if utilnet.IsIPv6(ip) {
		keepIPs := []*string{}
		for _, assignedIPv6 := range instance.NetworkInterfaces[0].Ipv6Addresses {
			if assignedIP := net.ParseIP(*assignedIPv6.Ipv6Address); assignedIP != nil && !assignedIP.Equal(ip) {
				keepIPs = append(keepIPs, assignedIPv6.Ipv6Address)
			}
		}
		input := ec2.AssignIpv6AddressesInput{
			NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
			Ipv6Addresses:      keepIPs,
		}
		_, err = a.client.AssignIpv6Addresses(&input)
		if err != nil {
			return err
		}
		return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
			instance, err := a.getInstance(node)
			return *instance.MetadataOptions.State == "applied", err
		})
	}
	keepIPs := []*string{}
	for _, assignedIPv4 := range instance.NetworkInterfaces[0].PrivateIpAddresses {
		if assignedIP := net.ParseIP(*assignedIPv4.PrivateIpAddress); assignedIP != nil && !assignedIP.Equal(ip) {
			keepIPs = append(keepIPs, assignedIPv4.PrivateIpAddress)
		}
	}
	inputV4 := ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
		PrivateIpAddresses: keepIPs,
	}
	_, err = a.client.AssignPrivateIpAddresses(&inputV4)
	if err != nil {
		return err
	}
	return wait.PollImmediate(cloudProviderPollInterval*time.Second, cloudProviderTimeoutDuration*time.Second, func() (bool, error) {
		instance, err := a.getInstance(node)
		return *instance.MetadataOptions.State == "applied", err
	})
}

func (a *AWS) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	instance, err := a.getInstance(node)
	if err != nil {
		return nil, nil, err
	}
	describeOutput, err := a.client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		SubnetIds: []*string{instance.SubnetId},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("error: cannot list ec2 subnets, err: %v", err)
	}
	if len(describeOutput.Subnets) > 1 {
		return nil, nil, fmt.Errorf("error: multiple subnets found for the subnet ID: %s", *instance.SubnetId)
	}
	var v4Subnet, v6Subnet *net.IPNet
	subnet := describeOutput.Subnets[0]
	if *subnet.CidrBlock != "" {
		_, subnet, err := net.ParseCIDR(*subnet.CidrBlock)
		if err != nil {
			return nil, nil, fmt.Errorf("error: unable to parse IPv4 subnet, err: %v", err)
		}
		v4Subnet = subnet
	}
	// I don't know what it means to have several IPv6 CIDR blocks defined for one subnet.
	// Let's just pick the first...
	if len(subnet.Ipv6CidrBlockAssociationSet) > 0 && *subnet.Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlock != "" {
		_, subnet, err := net.ParseCIDR(*subnet.Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlock)
		if err != nil {
			return nil, nil, fmt.Errorf("error: unable to parse IPv6 subnet, err: %v", err)
		}
		v6Subnet = subnet
	}

	return v4Subnet, v6Subnet, nil
}

//  This is what the node's providerID looks like on AWS
// 	spec:
//   providerID: aws:///us-west-2a/i-008447f243eead273
//  i.e: zone/instanceID
func (a *AWS) getInstance(node *corev1.Node) (*ec2.Instance, error) {
	providerData := parseProviderID(node.Spec.ProviderID)
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{awsapi.String(providerData[len(providerData)-1])},
	}
	result, err := a.client.DescribeInstances(input)
	if err != nil {
		return nil, fmt.Errorf("error: cannot list ec2 instance for node: %s, err: %v", node.Name, err)
	}
	instances := []*ec2.Instance{}
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, instance)
		}
	}
	if len(instances) != 1 {
		return nil, fmt.Errorf("error: found conflicting instance replicas for node: %s, instances: %v", node.Name, instances)
	}
	return instances[0], nil
}

func (a *AWS) watchForSecretChanges() {
	a.cloud.watchForSecretChanges()
}
