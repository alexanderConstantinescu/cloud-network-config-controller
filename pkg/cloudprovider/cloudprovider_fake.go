package cloudprovider

import (
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
)

func NewFakeCloudProvider(mockErrorOnAssign, mockErrorOnAssignWithExistingIPCondition, mockErrorOnRelease, mockErrorOnWait bool) *FakeCloudProvider {
	return &FakeCloudProvider{
		mockErrorOnAssign:                        mockErrorOnAssign,
		mockErrorOnAssignWithExistingIPCondition: mockErrorOnAssignWithExistingIPCondition,
		mockErrorOnRelease:                       mockErrorOnRelease,
		mockErrorOnWait:                          mockErrorOnWait,
	}
}

type FakeCloudProvider struct {
	mockErrorOnAssign                        bool
	mockErrorOnAssignWithExistingIPCondition bool
	mockErrorOnRelease                       bool
	mockErrorOnWait                          bool
	mockErrorOnGetNodeSubnet                 bool
}

func (f *FakeCloudProvider) initCredentials() error {
	return nil
}

func (f *FakeCloudProvider) AssignPrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	if f.mockErrorOnAssign {
		if f.mockErrorOnAssignWithExistingIPCondition {
			return nil, AlreadyExistingIPError
		}
		return nil, fmt.Errorf("Assign failed")
	}
	return nil, nil
}

func (f *FakeCloudProvider) ReleasePrivateIP(ip net.IP, node *corev1.Node) (interface{}, error) {
	if f.mockErrorOnRelease {
		return nil, fmt.Errorf("Release failed")
	}
	return nil, nil
}

func (f *FakeCloudProvider) WaitForResponse(_ interface{}) error {
	if f.mockErrorOnWait {
		return fmt.Errorf("Waiting failed")
	}
	return nil
}

func (f *FakeCloudProvider) GetNodeSubnet(node *corev1.Node) (*net.IPNet, *net.IPNet, error) {
	if f.mockErrorOnGetNodeSubnet {
		return nil, nil, fmt.Errorf("Get node subnet failed")
	}
	return nil, nil, nil
}
