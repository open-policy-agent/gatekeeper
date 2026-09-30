package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GVKManifestSpec struct {
	// groups maps an API group name to the versions and kinds it contains.
	Groups map[string]Versions `json:"groups,omitempty"`
}

type Versions map[string]Kinds

type Kinds []string

type Version struct {
	// name is the name of the API version.
	Name string `json:"name,omitempty"`
	// kinds lists the kinds available in this API version.
	Kinds []string `json:"kinds,omitempty"`
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true

// GVKManifest is the Schema for the GVKManifest API.
type GVKManifest struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of GVKManifest.
	Spec GVKManifestSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// GVKManifestList contains a list of GVKManifests.
type GVKManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GVKManifest `json:"items"`
}
