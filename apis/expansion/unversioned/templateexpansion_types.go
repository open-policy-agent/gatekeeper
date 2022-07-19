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
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TemplateExpansionSpec defines the desired state of TemplateExpansion.
type TemplateExpansionSpec struct {
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
}

type GeneratedGVK struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path="templateexpansion"
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:subresource:status

// TemplateExpansion is the Schema for the TemplateExpansion API.
type TemplateExpansion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TemplateExpansionSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TemplateExpansionList contains a list of TemplateExpansion.
type TemplateExpansionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplateExpansion `json:"items"`
}
