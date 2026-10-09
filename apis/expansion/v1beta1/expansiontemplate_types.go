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
	status "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ExpansionTemplateSpec defines the desired state of ExpansionTemplate.
type ExpansionTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// applyTo lists the specific groups, versions and kinds of generator resources
	// which will be expanded.
	ApplyTo []match.ApplyTo `json:"applyTo,omitempty"`

	// templateSource specifies the source field on the generator resource to
	// use as the base for expanded resource. For Pod-creating generators, this
	// is usually spec.template
	TemplateSource string `json:"templateSource,omitempty"`

	// generatedGVK specifies the GVK of the resources which the generator
	// resource creates.
	GeneratedGVK GeneratedGVK `json:"generatedGVK,omitempty"`

	// enforcementAction specifies the enforcement action to be used for resources
	// matching the ExpansionTemplate. Specifying an empty value will use the
	// enforcement action specified by the Constraint in violation.
	EnforcementAction string `json:"enforcementAction,omitempty"`
}

type GeneratedGVK struct {
	// group is the API group of the generated resource.
	Group string `json:"group,omitempty"`
	// version is the API version of the generated resource.
	Version string `json:"version,omitempty"`
	// kind is the kind of the generated resource.
	Kind string `json:"kind,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="expansiontemplate"
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// ExpansionTemplate is the Schema for the ExpansionTemplate API.
type ExpansionTemplate struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of ExpansionTemplate.
	Spec ExpansionTemplateSpec `json:"spec,omitempty"`
	// status is the observed state of ExpansionTemplate.
	Status ExpansionTemplateStatus `json:"status,omitempty"`
}

// ExpansionTemplateStatus defines the observed state of ExpansionTemplate.
type ExpansionTemplateStatus struct {
	// byPod lists the observed status of this ExpansionTemplate for each pod.
	ByPod []status.ExpansionTemplatePodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true

// ExpansionTemplateList contains a list of ExpansionTemplate.
type ExpansionTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExpansionTemplate `json:"items"`
}
