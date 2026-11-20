package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const AuditTriggerName = "audit-trigger"

// AuditTriggerSpec requests an immediate audit.
type AuditTriggerSpec struct {
	// auditNonce requests an audit as soon as possible when changed. It is a
	// change token, not an execution ID; use metadata.generation to track completion.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	AuditNonce string `json:"auditNonce"`
}

// AuditTriggerStatus describes the state of the requested audit.
type AuditTriggerStatus struct {
	// acknowledgedGeneration is the highest generation accepted by Gatekeeper.
	// +kubebuilder:validation:Minimum=0
	AcknowledgedGeneration int64 `json:"acknowledgedGeneration,omitempty"`
	// successfulGeneration is the highest generation covered by a successful audit.
	// Check this before FailedGeneration; a later failure does not invalidate success.
	// +kubebuilder:validation:Minimum=0
	SuccessfulGeneration int64 `json:"successfulGeneration,omitempty"`
	// failedGeneration is the highest generation covered by a failed audit.
	// +kubebuilder:validation:Minimum=0
	FailedGeneration int64 `json:"failedGeneration,omitempty"`
	// lastRun describes the most recently reported execution and is not authoritative for request completion.
	LastRun *AuditRunStatus `json:"lastRun,omitempty"`
}

// AuditRunStatus describes the most recently reported execution, regardless of its trigger.
// Audit expects a singleton audit pod; during pod overlap, lastRun may be overwritten out of order.
// Generation watermarks (auditTriggerStatus) remain monotonic and authoritative for request completion.
// Clients must use those watermarks, not lastRun, to determine whether a request is satisfied.
type AuditRunStatus struct {
	// coveredGeneration is frozen when the run starts. Later requests need another run.
	CoveredGeneration int64 `json:"coveredGeneration"`
	// startedAt is the time the audit run started.
	StartedAt metav1.Time `json:"startedAt"`
	// completedAt is the time the audit run finished, if a terminal result was reported.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
	// phase is the reported execution state; Running does not guarantee that the executing pod is still alive.
	// +kubebuilder:validation:Enum=Running;Succeeded;Failed;Unknown
	Phase string `json:"phase"`
	// reason is a machine-readable explanation of the reported state.
	Reason string `json:"reason"`
	// message is a human-readable explanation of the reported state.
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
	metav1.TypeMeta `json:",inline"`
	// metadata contains the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec requests an audit by changing its nonce.
	Spec AuditTriggerSpec `json:"spec"`
	// status reports generation watermarks and the most recently reported execution.
	Status AuditTriggerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AuditTriggerList contains a list of AuditTrigger.
type AuditTriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuditTrigger `json:"items"`
}
