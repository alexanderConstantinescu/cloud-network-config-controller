package controller

import (
	"fmt"
	"reflect"
	"testing"

	cloudprivateipconfig "github.com/openshift/cloud-network-config-controller/pkg/crd/cloudprivateipconfig/v1"
)

func TestIsValidSpec(t *testing.T) {
	tContainer := CloudPrivateIPConfigController{}
	tests := []struct {
		name   string
		input  []cloudprivateipconfig.CloudPrivateIPConfigItem
		expect error
	}{
		{
			name: "Should compute true for valid spec",
			input: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node1",
				},
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node2",
				},
			},
			expect: nil,
		},
		{
			name: "Should compute false for non-unique set",
			input: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node1",
				},
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node1",
				},
			},
			expect: fmt.Errorf("duplicate items found in the .spec stanza"),
		},
		{
			name: "Should compute false for IP is assigned to multiple nodes",
			input: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node1",
				},
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node2",
				},
			},
			expect: fmt.Errorf("IP: 192.168.126.13 has been defined on multiple nodes"),
		},
		{
			name: "Should compute false for invalid IP in spec",
			input: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.1653",
					Node: "node1",
				},
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node2",
				},
			},
			expect: fmt.Errorf("invalid IP address found in the .spec stanza: 192.168.126.1653"),
		},
	}
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%d:%s", i, tc.name), func(t *testing.T) {
			isValidSpec := tContainer.isValidSpec(tc.input)
			if !reflect.DeepEqual(isValidSpec, tc.expect) {
				t.Fatalf("Test case: %s, expected: %v, but had: %v", tc.name, tc.expect, isValidSpec)
			}
		})
	}
}

func TestComputeRequestDiff(t *testing.T) {
	tContainer := CloudPrivateIPConfigController{}
	tests := []struct {
		name                     string
		testCloudPrivateIPConfig cloudprivateipconfig.CloudPrivateIPConfig
		expectToAdd              []cloudprivateipconfig.CloudPrivateIPConfigItem
		expectToDel              []cloudprivateipconfig.CloudPrivateIPConfigItem
	}{
		{
			name: "Should compute correct diff for mutually exclusive sets",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.15",
							Node: "node3",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node1",
				},
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node2",
				},
			},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.15",
					Node: "node3",
				},
			},
		},
		{
			name: "Should compute correct diff for overlapping sets in add direction",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node1",
				},
			},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{},
		},
		{
			name: "Should compute correct diff for overlapping sets in del direction",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node2",
				},
			},
		},
		{
			name: "Should compute correct diff for equal sets",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{},
		},
		{
			name: "Should compute correct diff for IP change in sets",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.13",
					Node: "node1",
				},
			},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node1",
				},
			},
		},
		{
			name: "Should compute correct diff for node change in sets",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node3",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node1",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node3",
				},
			},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node1",
				},
			},
		},
		{
			name: "Should compute correct diff for incorrect IP definition in sets",
			testCloudPrivateIPConfig: cloudprivateipconfig.CloudPrivateIPConfig{
				Spec: cloudprivateipconfig.CloudPrivateIPConfigSpec{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.12",
							Node: "node3",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
				Status: cloudprivateipconfig.CloudPrivateIPConfigStatus{
					Items: []cloudprivateipconfig.CloudPrivateIPConfigItem{
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.1652",
							Node: "node3",
						},
						cloudprivateipconfig.CloudPrivateIPConfigItem{
							IP:   "192.168.126.13",
							Node: "node2",
						},
					},
				},
			},
			expectToAdd: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.12",
					Node: "node3",
				},
			},
			expectToDel: []cloudprivateipconfig.CloudPrivateIPConfigItem{
				cloudprivateipconfig.CloudPrivateIPConfigItem{
					IP:   "192.168.126.1652",
					Node: "node3",
				},
			},
		},
	}
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%d:%s", i, tc.name), func(t *testing.T) {
			resToAdd, resToDel := tContainer.computeRequestDiff(&tc.testCloudPrivateIPConfig)
			if !reflect.DeepEqual(resToAdd, tc.expectToAdd) {
				t.Fatalf("Test case: %s, expected: %v, but had: %v", tc.name, tc.expectToAdd, resToAdd)
			} else if !reflect.DeepEqual(resToDel, tc.expectToDel) {
				t.Fatalf("Test case: %s, expected: %v, but had: %v", tc.name, tc.expectToDel, resToDel)
			}
		})
	}
}
