package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SyncSetPodStatusStatus defines the observed state of SyncSetPodStatus.
type SyncSetPodStatusStatus struct {
	ID                 string `json:"id,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// SyncSetPodStatus is the Schema for the syncsetpodstatuses API.
type SyncSetPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status SyncSetPodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SyncSetPodStatusList contains a lit of SyncSetPodStatus.
type SyncSetPodStatusList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Items             []SyncSetPodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SyncSetPodStatus{}, &SyncSetPodStatusList{})
}
