package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const AuditTriggerName = "audit-trigger"

// AuditTriggerSpec requests an immediate audit.
type AuditTriggerSpec struct {
	// AuditNonce requests an audit as soon as possible when changed. It is a
	// change token, not an execution ID; use metadata.generation to track completion.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	AuditNonce string `json:"auditNonce"`
}

// AuditTriggerStatus describes the state of the requested audit.
type AuditTriggerStatus struct {
	// AcknowledgedGeneration is the highest generation accepted by Gatekeeper.
	// +kubebuilder:validation:Minimum=0
	AcknowledgedGeneration int64 `json:"acknowledgedGeneration,omitempty"`
	// SuccessfulGeneration is the highest generation covered by a successful audit.
	// Check this before FailedGeneration; a later failure does not invalidate success.
	// +kubebuilder:validation:Minimum=0
	SuccessfulGeneration int64 `json:"successfulGeneration,omitempty"`
	// FailedGeneration is the highest generation covered by a failed audit.
	// +kubebuilder:validation:Minimum=0
	FailedGeneration int64           `json:"failedGeneration,omitempty"`
	LastRun          *AuditRunStatus `json:"lastRun,omitempty"`
}

// AuditRunStatus describes the latest started execution, regardless of its trigger.
type AuditRunStatus struct {
	// CoveredGeneration is frozen when the run starts. Later requests need another run.
	CoveredGeneration int64        `json:"coveredGeneration"`
	StartedAt         metav1.Time  `json:"startedAt"`
	CompletedAt       *metav1.Time `json:"completedAt,omitempty"`
	// +kubebuilder:validation:Enum=Running;Succeeded;Failed;Unknown
	Phase   string `json:"phase"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Acknowledged",type="integer",JSONPath=".status.acknowledgedGeneration"
// +kubebuilder:printcolumn:name="Succeeded",type="integer",JSONPath=".status.successfulGeneration"
// +kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.failedGeneration"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'audit-trigger'",message="AuditTrigger must be named audit-trigger"

// AuditTrigger requests an audit as soon as possible.
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
