package cloudprovider

import (
	"fmt"
	"net"

	awsapi "github.com/aws/aws-sdk-go/aws"
	awscredentials "github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	corev1 "k8s.io/api/core/v1"
	utilnet "k8s.io/utils/net"
)

const (
	aws = "aws"
)

var (
	awsIPv4FilterKey = "network-interface.addresses.private-ip-address"
	awsIPv6FilterKey = "network-interface.ipv6-addresses.ipv6-address"
)

// AWS implements the API wrapper for talking to the AWS cloud API
type AWS struct {
	CloudProvider
	region string
	client *ec2.EC2
}

// AWSWaitInput is the required input for the AWS EC2 Wait API call (WaitUntilInstanceRunning).
// Unfortunately the API only handles equality assertion: so on delete we can't
// specify and assert that the IP which is being removed is completely removed,
// we are forced to do the inverse, i.e: assert that all IPs except the IP being
// removed are there (which should anyways be the case). Hence we might risk
// having a very small time window where the state is incorrect. This can be
// problematic for update operation. The assumption here is that AWS EC2 will be
// able to handle this. Unfortunately, my research showed that there is no other
// API call that we can use to avoid this.
type AWSWaitInput struct {
	instanceID *string
	ips        []*string
}

func (a *AWS) initCredentials() error {
	accessKey, err := a.readSecretData("aws_access_key_id")
	if err != nil {
		return err
	}
	secretKey, err := a.readSecretData("aws_secret_access_key")
	if err != nil {
		return err
	}
	mySession := session.Must(session.NewSession())
	a.client = ec2.New(mySession, awsapi.NewConfig().WithCredentials(awscredentials.NewStaticCredentials(accessKey, secretKey, "")).WithRegion(a.region))
	return nil
}

func (a *AWS) AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	instance, err := a.getInstance(node)
	if err != nil {
		return nil, err
	}
	addIP := ip.String()
	keepIPs := []*string{}
	if utilnet.IsIPv6(ip) {
		for _, assignedIPv6 := range instance.NetworkInterfaces[0].Ipv6Addresses {
			if assignedIP := net.ParseIP(*assignedIPv6.Ipv6Address); assignedIP != nil && assignedIP.Equal(ip) {
				return nil, AlreadyExistingIPError
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
			return nil, err
		}
		awsWaitInput := AWSWaitInput{
			instanceID: instance.InstanceId,
			ips:        keepIPs,
		}
		return awsWaitInput, nil
	}
	for _, assignedIPv4 := range instance.NetworkInterfaces[0].PrivateIpAddresses {
		if assignedIP := net.ParseIP(*assignedIPv4.PrivateIpAddress); assignedIP != nil && assignedIP.Equal(ip) {
			return nil, AlreadyExistingIPError
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
		return nil, err
	}
	awsWaitInput := AWSWaitInput{
		instanceID: instance.InstanceId,
		ips:        keepIPs,
	}
	return awsWaitInput, nil
}

func (a *AWS) ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	instance, err := a.getInstance(node)
	if err != nil {
		return nil, err
	}
	deleteIPs := []*string{}
	keepIPs := []*string{}
	if utilnet.IsIPv6(ip) {
		for _, assignedIPv6 := range instance.NetworkInterfaces[0].Ipv6Addresses {
			if assignedIP := net.ParseIP(*assignedIPv6.Ipv6Address); assignedIP != nil && assignedIP.Equal(ip) {
				deleteIPs = append(deleteIPs, assignedIPv6.Ipv6Address)
			} else {
				keepIPs = append(keepIPs, assignedIPv6.Ipv6Address)
			}
		}
		input := ec2.UnassignIpv6AddressesInput{
			NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
			Ipv6Addresses:      deleteIPs,
		}
		_, err = a.client.UnassignIpv6Addresses(&input)
		if err != nil {
			return nil, err
		}
		awsWaitInput := AWSWaitInput{
			instanceID: instance.InstanceId,
			ips:        keepIPs,
		}
		return awsWaitInput, nil
	}
	for _, assignedIPv4 := range instance.NetworkInterfaces[0].PrivateIpAddresses {
		if assignedIP := net.ParseIP(*assignedIPv4.PrivateIpAddress); assignedIP != nil && assignedIP.Equal(ip) {
			deleteIPs = append(deleteIPs, assignedIPv4.PrivateIpAddress)
		} else {
			keepIPs = append(keepIPs, assignedIPv4.PrivateIpAddress)
		}
	}
	inputV4 := ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: instance.NetworkInterfaces[0].NetworkInterfaceId,
		PrivateIpAddresses: deleteIPs,
	}
	_, err = a.client.UnassignPrivateIpAddresses(&inputV4)
	if err != nil {
		return nil, err
	}
	awsWaitInput := AWSWaitInput{
		instanceID: instance.InstanceId,
		ips:        keepIPs,
	}
	return awsWaitInput, nil
}

func (a *AWS) WaitForResponse(requestObj interface{}) error {
	awsWaitInput, ok := requestObj.(AWSWaitInput)
	if !ok {
		return fmt.Errorf("error decoding AWS requestObj, object not of type: AWSWaitInput %#v", requestObj)
	}
	var ec2IPFilter string
	sampleIP := *awsWaitInput.ips[0]
	if utilnet.IsIPv6String(sampleIP) {
		ec2IPFilter = awsIPv6FilterKey
	} else {
		ec2IPFilter = awsIPv4FilterKey
	}
	err := a.client.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{awsWaitInput.instanceID},
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   &ec2IPFilter,
				Values: awsWaitInput.ips,
			},
		},
	})
	return err
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

	// I don't know what it means to have several IPv6 CIDR blocks defined for
	// one subnet, specially given that you can only have one IPv4 CIDR block
	// defined...¯\_(ツ)_/¯
	// Let's just pick the first.
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
