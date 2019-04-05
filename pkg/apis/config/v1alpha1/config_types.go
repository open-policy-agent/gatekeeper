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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ConfigSpec defines the desired state of Config
type ConfigSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Configuration for syncing k8s objects
	Sync Sync `json:"sync,omitempty"`
}

type Sync struct {
	// If non-empty, only entries on this list will be replicated into OPA
	SyncOnly []SyncOnlyEntry `json:"syncOnly,omitempty"`
}

type SyncOnlyEntry struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// ConfigStatus defines the observed state of Config
type ConfigStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// List of Group/Version/Kinds with finalizers
	AllFinalizers []GVK `json:"allFinalizers,omitempty"`
}

func ToAPIGVK(gvk schema.GroupVersionKind) GVK {
	return GVK{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind}
}

func ToGVK(gvk GVK) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind}
}

type GVK struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Config is the Schema for the configs API
// +k8s:openapi-gen=true
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigSpec   `json:"spec,omitempty"`
	Status ConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

// ConfigList contains a list of Config
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Config `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Config{}, &ConfigList{})
}
