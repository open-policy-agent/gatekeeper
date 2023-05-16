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

// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SyncSet is the Schema for the SyncSet API.
type SyncSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SyncSetSpec   `json:"spec,omitempty"`
	Status SyncSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SyncSetList contains a list of SyncSet.
type SyncSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SyncSet `json:"items"`
}

type SyncSetStatus struct {
	ByPod []SyncSetStatusStatus `json:"byPod,omitempty"`
}

type SyncSetStatusStatus struct {
	ID       string `json:"id,omitempty"`
	Ingested bool   `json:"ingested,omitempty"`
}

func init() {
	SchemeBuilder.Register(&SyncSet{}, &SyncSetList{})
}
