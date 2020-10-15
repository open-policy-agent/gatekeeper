/*

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

package v1alpha1

import (
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AssignMetadataSpec defines the desired state of AssignMetadata
type AssignMetadataSpec struct {
	Match      Match      `json:"match,omitempty"`
	Location   string     `json:"location,omitempty"`
	Parameters Parameters `json:"parameters,omitempty"`
}

// AssignMetadataStatus defines the observed state of AssignMetadata
type AssignMetadataStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

type Match struct {
	Kinds              []Kinds                            `json:"kinds,omitempty"`
	Scope              apiextensionsv1beta1.ResourceScope `json:"scope" protobuf:"bytes,4,opt,name=scope,casttype=ResourceScope"`
	Namespaces         []string                           `json:"namespaces,omitempty"`
	ExcludedNamespaces []string                           `json:"excludedNamespaces,omitempty"`
	LabelSelector      *metav1.LabelSelector              `json:"labelSelector,omitempty"`
	NamespaceSelector  *metav1.LabelSelector              `json:"namespaceSelector,omitempty"`
}

// Kinds accepts a list of objects with apiGroups and kinds fields
// that list the groups/kinds of objects to which the mutation will apply.
// If multiple groups/kinds objects are specified,
// only one match is needed for the resource to be in scope.
type Kinds struct {
	// APIGroups is the API groups the resources belong to. '*' is all groups.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIGroups []string `json:"apiGroups,omitempty" protobuf:"bytes,1,rep,name=apiGroups"`
	Kinds     []string `json:"kinds,omitempty"`
}

type Parameters struct {
	Value string `json:"value,omitempty"`
}

// +kubebuilder:object:root=true
// TODO: check if we need to define the scope here (e.g kubebuilder:resource:scope=Namespaced)
// TODO:  check addition of k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AssignMetadata is the Schema for the assignmetadata API
type AssignMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignMetadataSpec   `json:"spec,omitempty"`
	Status AssignMetadataStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// TODO:  check addition of k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AssignMetadataList contains a list of AssignMetadata
type AssignMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AssignMetadata `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AssignMetadata{}, &AssignMetadataList{})
}
