package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GVKManifestSpec struct {
	Groups map[string]Versions `json:"groups,omitempty"`
}

type Versions map[string]Kinds

type Kinds []string

type Version struct {
	Name  string   `json:"name,omitempty"`
	Kinds []string `json:"kinds,omitempty"`
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true

// GVKManifest is the Schema for the GVKManifest API.
type GVKManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GVKManifestSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// GVKManifestList contains a list of GVKManifests.
type GVKManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GVKManifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GVKManifest{}, &GVKManifestList{})
}
