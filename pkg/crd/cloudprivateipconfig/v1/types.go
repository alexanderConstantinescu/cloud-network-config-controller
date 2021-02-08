package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:noStatus
// +resource:path=cloudprivateipconfig
// +kubebuilder:resource:shortName=cpip,scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:name="Node Request",type=string,JSONPath=".spec.items[*].node"
// +kubebuilder:printcolumn:name="IP Request",type=string,JSONPath=".spec.items[*].ip"
// +kubebuilder:printcolumn:name="Node Assignment",type=string,JSONPath=".status.items[*].node"
// +kubebuilder:printcolumn:name="IP Assignment",type=string,JSONPath=".status.items[*].ip"
// CloudPrivateIPConfig is a CRD allowing the user to assign private
// IP addresses to the primary NIC on cloud VMs. This is done by
// specifying the spec with the Kubernetes node(s) the request
// is meant for, as well as the IP(s) requested for that/those
// node(s).
type CloudPrivateIPConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired private IP request(s).
	Spec CloudPrivateIPConfigSpec `json:"spec"`
	// Observed status of CloudPrivateIPConfig. Read-only.
	// +optional
	Status CloudPrivateIPConfigStatus `json:"status,omitempty"`
}

type CloudPrivateIPConfigSpec struct {
	// The list of per node requested private IPs and their corresponding node assignment.
	Items []CloudPrivateIPConfigItem `json:"items"`
}

type CloudPrivateIPConfigStatus struct {
	// The list of per node status of private IP assignments
	// executed. In case an assignment fails: the item will be omitted from the
	// status.items. I.e: if all went well the `.spec` should
	// equal `.status`
	Items []CloudPrivateIPConfigItem `json:"items"`
}

type CloudPrivateIPConfigItem struct {
	// Node name
	Node string `json:"node"`
	// IP address - can be IPv4 or IPv6
	IP string `json:"ip"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resource:path=cloudprivateipconfig
// CloudPrivateIPConfigList is the list of CloudPrivateIPConfigList.
type CloudPrivateIPConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of CloudPrivateIPConfig.
	Items []CloudPrivateIPConfig `json:"items"`
}
