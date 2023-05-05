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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ModifySetSpec defines the desired state of ModifySet.
type ModifySetSpec struct {
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

	// Location describes the path to be mutated, for example: `spec.containers[name: main].args`.
	Location string `json:"location,omitempty"`

	// Parameters define the behavior of the mutator.
	Parameters ModifySetParameters `json:"parameters,omitempty"`
}

type ModifySetParameters struct {
	// PathTests are a series of existence tests that can be checked
	// before a mutation is applied
	PathTests []PathTest `json:"pathTests,omitempty"`

	// Operation describes whether values should be merged in ("merge"), or pruned ("prune"). Default value is "merge"
	// +kubebuilder:validation:Enum=merge;prune
	// +kubebuilder:default=merge
	Operation Operation `json:"operation,omitempty"`

	// Values describes the values provided to the operation as `values.fromList`.
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:XPreserveUnknownFields
	Values Values `json:"values,omitempty"`
}

type Operation string

const (
	// MergeOp means that the provided values should be merged with the existing values.
	MergeOp Operation = "merge"

	// PruneOp means that the provided values should be removed from the existing values.
	PruneOp Operation = "prune"
)

// Values describes the values provided to the operation.
// +kubebuilder:object:generate=false
type Values struct {
	FromList []interface{} `json:"fromList,omitempty"`
}

func (in *Values) DeepCopy() *Values {
	if in == nil {
		return nil
	}

	var fromList []interface{}
	if in.FromList != nil {
		fromList = make([]interface{}, len(in.FromList))
		for i := range fromList {
			fromList[i] = runtime.DeepCopyJSONValue(in.FromList[i])
		}
	}

	return &Values{
		FromList: fromList,
	}
}

func (in *Values) DeepCopyInto(out *Values) {
	*in = *out

	if in.FromList != nil {
		fromList := make([]interface{}, len(in.FromList))
		for i := range fromList {
			fromList[i] = runtime.DeepCopyJSONValue(in.FromList[i])
		}
		out.FromList = fromList
	}
}

// ModifySetStatus defines the observed state of ModifySet.
type ModifySetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ByPod []v1beta1.MutatorPodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="modifyset"
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// ModifySet allows the user to modify non-keyed lists, such as
// the list of arguments to a container.
type ModifySet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModifySetSpec   `json:"spec,omitempty"`
	Status ModifySetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModifySetList contains a list of ModifySet.
type ModifySetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModifySet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModifySet{}, &ModifySetList{})
}
