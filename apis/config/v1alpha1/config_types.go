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
	status "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ConfigSpec defines the desired state of Config.
type ConfigSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// sync is the configuration for syncing k8s objects
	Sync Sync `json:"sync,omitempty"`

	// validation is the configuration for validation
	Validation Validation `json:"validation,omitempty"`

	// match is the configuration for namespace exclusion
	Match []MatchEntry `json:"match,omitempty"`

	// readiness is the configuration for readiness tracker
	Readiness ReadinessSpec `json:"readiness,omitempty"`
}

type Validation struct {
	// traces is the list of requests to trace. Both "user" and "kinds" must be specified
	Traces []Trace `json:"traces,omitempty"`
}

type Trace struct {
	// user restricts tracing to requests from the specified user
	User string `json:"user,omitempty"`
	// kind restricts tracing to requests of the following GroupVersionKind
	Kind GVK `json:"kind,omitempty"`
	// dump also dumps the state of OPA with the trace. Set to `All` to dump everything.
	Dump string `json:"dump,omitempty"`
}

type Sync struct {
	// syncOnly restricts replication into OPA to only the entries on this list,
	// when non-empty
	SyncOnly []SyncOnlyEntry `json:"syncOnly,omitempty"`
}

type SyncOnlyEntry struct {
	// group is the API group of the resource to sync.
	Group string `json:"group,omitempty"`
	// version is the API version of the resource to sync.
	Version string `json:"version,omitempty"`
	// kind is the kind of the resource to sync.
	Kind string `json:"kind,omitempty"`
}

func (e *SyncOnlyEntry) ToGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   e.Group,
		Version: e.Version,
		Kind:    e.Kind,
	}
}

type MatchEntry struct {
	// processes lists the Gatekeeper processes this exclusion applies to.
	Processes []string `json:"processes,omitempty"`
	// excludedNamespaces lists the namespaces excluded from the listed processes.
	ExcludedNamespaces []wildcard.Wildcard `json:"excludedNamespaces,omitempty"`
}

type ReadinessSpec struct {
	// statsEnabled enables reporting of readiness tracker statistics.
	StatsEnabled bool `json:"statsEnabled,omitempty"`
}

// ConfigStatus defines the observed state of Config.
type ConfigStatus struct { // Important: Run "make" to regenerate code after modifying this file
	// byPod lists the observed status of this Config for each pod.
	ByPod []status.ConfigPodStatusStatus `json:"byPod,omitempty"`
}

type GVK struct {
	// group is the API group of the resource.
	Group string `json:"group,omitempty"`
	// version is the API version of the resource.
	Version string `json:"version,omitempty"`
	// kind is the kind of the resource.
	Kind string `json:"kind,omitempty"`
}

// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// Config is the Schema for the configs API.
type Config struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Config.
	Spec ConfigSpec `json:"spec,omitempty"`
	// status is the observed state of Config.
	Status ConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigList contains a list of Config.
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Config `json:"items"`
}
