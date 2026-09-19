package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type SyncSetSpec struct {
	// gvks lists the group, version and kind of the resources to be synced.
	GVKs []GVKEntry `json:"gvks,omitempty"`
}

type GVKEntry struct {
	// group is the API group of the resource to sync.
	Group string `json:"group,omitempty"`
	// version is the API version of the resource to sync.
	Version string `json:"version,omitempty"`
	// kind is the kind of the resource to sync.
	Kind string `json:"kind,omitempty"`
}

func (e *GVKEntry) ToGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   e.Group,
		Version: e.Version,
		Kind:    e.Kind,
	}
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true

// SyncSet defines which resources Gatekeeper will cache. The union of all SyncSets plus the syncOnly field of Gatekeeper's Config resource defines the sets of resources that will be synced.
type SyncSet struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of SyncSet.
	Spec SyncSetSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// SyncSetList contains a list of SyncSet.
type SyncSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SyncSet `json:"items"`
}
