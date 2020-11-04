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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AssignSpec defines the desired state of Assign
type AssignSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ApplyTo    []ApplyTo  `json:"applyTo,omitempty"`
	Match      Match      `json:"match,omitempty"`
	Location   string     `json:"location,omitempty"`
	Parameters Parameters `json:"parameters,omitempty"`
}

// ApplyTo determines what GVKs items the mutation should apply to.
// Globs are not allowed.
type ApplyTo struct {
	Groups   []string `json:"groups,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

type Parameters struct {
	PathTests []PathTest `json:"pathTests,omitempty"`
	// IfIn Only mutate if the current value is in the supplied list
	IfIn []string `json:"ifIn,omitempty"`
	// IfNotIn Only mutate if the current value is NOT in the supplied list
	IfNotIn []string `json:"ifNotIn,omitempty"`
	// Assign.value holds the value to be assigned
	// +kubebuilder:validation:XPreserveUnknownFields
	Assign runtime.RawExtension `json:"assign,omitempty"`
}

// +kubebuilder:validation:Enum=MustExist;MustNotExist
type Condition string

// PathTests allows the user to customize how the mutation works if parent
// paths are missing. It traverses the list in order. All sub paths are
// tested against the provided condition, if the test fails, the mutation is
// not applied. All `subPath` entries must be a prefix of `location`. Any
// glob characters will take on the same value as was used to
// expand the matching glob in `location`.
//
// Available Tests:
// * MustExist    - the path must exist or do not mutate
// * MustNotExist - the path must not exist or do not mutate
type PathTest struct {
	SubPath   string    `json:"subPath,omitempty"`
	Condition Condition `json:"condition,omitempty"`
}

// AssignStatus defines the observed state of Assign
type AssignStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="assign"
// +kubebuilder:resource:scope="Cluster"

// Assign is the Schema for the assign API
type Assign struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignSpec   `json:"spec,omitempty"`
	Status AssignStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AssignList contains a list of Assign
type AssignList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Assign `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Assign{}, &AssignList{})
}
