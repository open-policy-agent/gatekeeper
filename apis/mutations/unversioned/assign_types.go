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

package unversioned

import (
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AssignSpec defines the desired state of Assign.
type AssignSpec struct {
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

	// Location describes the path to be mutated, for example: `spec.containers[name: main]`.
	Location string `json:"location,omitempty"`

	// Parameters define the behavior of the mutator.
	Parameters Parameters `json:"parameters,omitempty"`
}

type Parameters struct {
	PathTests []PathTest `json:"pathTests,omitempty"`

	// TODO(maxsmythe): Now that https://github.com/kubernetes-sigs/controller-tools/pull/528
	// is merged, we can use an actual object for `Assign`

	// Assign.value holds the value to be assigned
	Assign AssignField `json:"assign,omitempty"`
}

// PathTest allows the user to customize how the mutation works if parent
// paths are missing. It traverses the list in order. All sub paths are
// tested against the provided condition, if the test fails, the mutation is
// not applied. All `subPath` entries must be a prefix of `location`. Any
// glob characters will take on the same value as was used to
// expand the matching glob in `location`.
//
// Available Tests:
// * MustExist    - the path must exist or do not mutate
// * MustNotExist - the path must not exist or do not mutate.
type PathTest struct {
	SubPath   string           `json:"subPath,omitempty"`
	Condition tester.Condition `json:"condition,omitempty"`
}

// AssignStatus defines the observed state of Assign.
type AssignStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ByPod []v1beta1.MutatorPodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true

// Assign is the Schema for the assign API.
type Assign struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignSpec   `json:"spec,omitempty"`
	Status AssignStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AssignList contains a list of Assign.
type AssignList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Assign `json:"items"`
}
