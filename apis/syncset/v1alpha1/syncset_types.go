package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SyncSetSpec struct {
	GVKs []GVKEntry `json:"gvks,omitempty"`
}

type GVKEntry struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true

// SyncSet is the Schema for the SyncSet API.
type SyncSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SyncSetSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// SyncSetList contains a list of SyncSet.
type SyncSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SyncSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SyncSet{}, &SyncSetList{})
}
