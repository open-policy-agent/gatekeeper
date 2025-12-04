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
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ExpansionTemplateSpec defines the desired state of ExpansionTemplate.
type ExpansionTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ApplyTo lists the specific groups, versions and kinds of generator resources
	// which will be expanded.
	ApplyTo []match.ApplyTo `json:"applyTo,omitempty"`

	// TemplateSource specifies the source field on the generator resource to
	// use as the base for expanded resource. For Pod-creating generators, this
	// is usually spec.template
	TemplateSource string `json:"templateSource,omitempty"`

	// GeneratedGVK specifies the GVK of the resources which the generator
	// resource creates.
	GeneratedGVK GeneratedGVK `json:"generatedGVK,omitempty"`

	// EnforcementAction specifies the enforcement action to be used for resources
	// matching the ExpansionTemplate. Specifying an empty value will use the
	// enforcement action specified by the Constraint in violation.
	EnforcementAction string `json:"enforcementAction,omitempty"`
}

type GeneratedGVK struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="expansiontemplate"
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// ExpansionTemplate is the Schema for the ExpansionTemplate API.
type ExpansionTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExpansionTemplateSpec   `json:"spec,omitempty"`
	Status ExpansionTemplateStatus `json:"status,omitempty"`
}

// ExpansionTemplateStatus defines the observed state of ExpansionTemplate.
type ExpansionTemplateStatus struct {
	// +optional
	ByPod []statusv1alpha1.ExpansionTemplatePodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true

// ExpansionTemplateList contains a list of ExpansionTemplate.
type ExpansionTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExpansionTemplate `json:"items"`
}
