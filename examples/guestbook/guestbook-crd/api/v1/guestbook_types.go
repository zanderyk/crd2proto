/*
Copyright 2026.

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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	virtv1 "kubevirt.io/api/core/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GuestbookSpec defines the desired state of Guestbook
// +k8s:protobuf-gen=true
type GuestbookSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of Guestbook. Edit guestbook_types.go to remove/update
	// +optional
	Foo *string `json:"foo,omitempty" protobuf:"bytes,1,opt,name=foo"`
	// Bar is a int64 value! Who knows what it does!
	Bar *int64 `json:"bar,omitempty" protobuf:"varint,2,opt,name=bar"`
	// This is a pod spec
	Pod corev1.Pod `json:"pod,omitempty" protobuf:"bytes,3,opt,name=pod"`
	// VirtualMachine
	VirtualMachine virtv1.VirtualMachine `json:"virtualMachine" protobuf:"bytes,4,opt,name=virtualMachine"`
}

// GuestbookStatus defines the observed state of Guestbook.
// +k8s:protobuf-gen=true
type GuestbookStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Guestbook resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Guestbook is the Schema for the guestbooks API
// +k8s:protobuf-gen=true
type Guestbook struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero" protobuf:"bytes,1,opt,name=metadata"`

	// spec defines the desired state of Guestbook
	// +required
	Spec GuestbookSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// status defines the observed state of Guestbook
	// +optional
	Status GuestbookStatus `json:"status,omitzero" protobuf:"bytes,3,opt,name=status"`
}

// +kubebuilder:object:root=true

// GuestbookList contains a list of Guestbook
// +k8s:protobuf-gen=true
type GuestbookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Guestbook `json:"items" protobuf:"bytes,2,rep,name=items"`
}
