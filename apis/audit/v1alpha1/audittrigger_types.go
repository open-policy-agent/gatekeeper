package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	ConditionAcknowledged = "Acknowledged"
	ConditionSucceeded    = "Succeeded"
)

// AuditTriggerSpec defines when an audit should run.
type AuditTriggerSpec struct {
	// Timestamp is the earliest time at which to start the requested audit.
	Timestamp metav1.Time `json:"timestamp"`
}

// AuditTriggerStatus describes the state of the requested audit.
type AuditTriggerStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Acknowledged",type="string",JSONPath=".status.conditions[?(@.type=='Acknowledged')].status"
// +kubebuilder:printcolumn:name="Succeeded",type="string",JSONPath=".status.conditions[?(@.type=='Succeeded')].status"

// AuditTrigger requests an audit at or after a specified time.
type AuditTrigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuditTriggerSpec   `json:"spec"`
	Status AuditTriggerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AuditTriggerList contains a list of AuditTrigger.
type AuditTriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuditTrigger `json:"items"`
}
