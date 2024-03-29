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

package v1beta1

import (
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AssignMetadataSpec defines the desired state of AssignMetadata.
type AssignMetadataSpec struct {
	Match      match.Match        `json:"match,omitempty"`
	Location   string             `json:"location,omitempty"`
	Parameters MetadataParameters `json:"parameters,omitempty"`
}

type MetadataParameters struct {
	// Assign.value holds the value to be assigned
	Assign AssignField `json:"assign,omitempty"`
}

// AssignMetadataStatus defines the observed state of AssignMetadata.
type AssignMetadataStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ByPod []v1beta1.MutatorPodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// AssignMetadata is the Schema for the assignmetadata API.
type AssignMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignMetadataSpec   `json:"spec,omitempty"`
	Status AssignMetadataStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AssignMetadataList contains a list of AssignMetadata.
type AssignMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AssignMetadata `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AssignMetadata{}, &AssignMetadataList{})
}
