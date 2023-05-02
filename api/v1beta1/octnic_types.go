/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OctNicSpec defines the desired state of OctNic
type OctNicSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Acclr    string `json:"acclr,omitempty"`
	NodeName string `json:"nodename,omitempty"`

	// Device configuration
	PciAddr string `json:"pciAddr,omitempty"`
	NumVfs  string `json:"numvfs,omitempty"`
	/*
		Pass resource names and their mappings through CRD
		Syntax:
		resourcename:
		  - "marvell_sriov_net_vamp#0"
		  - "marvell_sriov_net_rmp#8-15"
		  - "marvell_sriov_net_dip#20-21"
		  - "marvell_sriov_net_dpp#32,36-37,40-47"
	*/
	ResourceName   []string `json:"resourceName,omitempty"`
	ResourcePrefix string   `json:"resourcePrefix,omitempty"`

	// The SDK version refers to runtime FW and Rootfs
	// Versions on the OctNic. This SDK version is used
	// as a TAG to identify the tools update image when
	// updating the device's FW and rootfs
	SDKVersion string `json:"fwimage,omitempty"`
}

type OctNicOperationState string

const (
	// Unknown state
	OctS0 OctNicOperationState = "FW Update"
	// Driver Loaded
	OctS1 OctNicOperationState = "Driver Bind"
	// Driver Validate
	OctS2 OctNicOperationState = "DriverValidate"
	// DP Loaded
	OctS3 OctNicOperationState = "DpLoaded"
	// DP Validate
	OctS4 OctNicOperationState = "DpValidate"
	// Run
	OctS5 OctNicOperationState = "Run"
)

type OctNicDev struct {
	NodeName string               `json:"nodename,omitempty"`
	PciAddr  string               `json:"pciAddr,omitempty"`
	OpState  OctNicOperationState `json:"opstate,omitempty"`
}

// OctNicStatus defines the observed state of OctNic
type OctNicStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	OctNicState []OctNicDev `json:"OctNicState,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OctNic is the Schema for the octnics API
type OctNic struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OctNicSpec   `json:"spec,omitempty"`
	Status OctNicStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OctNicList contains a list of OctNic
type OctNicList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OctNic `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OctNic{}, &OctNicList{})
}
