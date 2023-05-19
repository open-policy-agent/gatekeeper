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
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AssignImageSpec defines the desired state of AssignImage.
type AssignImageSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ApplyTo lists the specific groups, versions and kinds a mutation will be applied to.
	// This is necessary because every mutation implies part of an object schema and object
	// schemas are associated with specific GVKs.
	ApplyTo []match.ApplyTo `json:"applyTo,omitempty"`

	// Match allows the user to limit which resources get mutated.
	// Individual match criteria are AND-ed together. An undefined
	// match criteria matches everything.
	Match match.Match `json:"match,omitempty"`

	// Location describes the path to be mutated, for example: `spec.containers[name: main].image`.
	Location string `json:"location,omitempty"`

	// Parameters define the behavior of the mutator.
	Parameters AssignImageParameters `json:"parameters,omitempty"`
}

type AssignImageParameters struct {
	PathTests []PathTest `json:"pathTests,omitempty"`

	// AssignDomain sets the domain component on an image string. The trailing
	// slash should not be included.
	AssignDomain string `json:"assignDomain,omitempty"`

	// AssignPath sets the domain component on an image string.
	AssignPath string `json:"assignPath,omitempty"`

	// AssignImage sets the image component on an image string. It must start
	// with a `:` or `@`.
	AssignTag string `json:"assignTag,omitempty"`
}

// AssignImageStatus defines the observed state of AssignImage.
type AssignImageStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ByPod []v1beta1.MutatorPodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="assignimage"
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// AssignImage is the Schema for the assignimage API.
type AssignImage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignImageSpec   `json:"spec,omitempty"`
	Status AssignImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AssignImageList contains a list of AssignImage.
type AssignImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AssignImage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AssignImage{}, &AssignImageList{})
}
