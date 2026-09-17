package util

import "flag"

const (
	// EmitAdmissionAuditAnnotationsFlag is the CLI flag that enables admission audit annotations.
	EmitAdmissionAuditAnnotationsFlag           = "emit-admission-audit-annotations"
	AdmissionAuditAnnotationsViolationsOnlyFlag = "admission-audit-annotations-violations-only"
)

var (
	emitAdmissionAuditAnnotations = flag.Bool(
		EmitAdmissionAuditAnnotationsFlag,
		false,
		"(alpha) emit API server audit annotations for evaluated validation requests and Gatekeeper-generated ValidatingAdmissionPolicy evaluations",
	)
	admissionAuditAnnotationsViolationsOnly = flag.Bool(
		AdmissionAuditAnnotationsViolationsOnlyFlag,
		false,
		"(alpha) when admission audit annotations are enabled, omit webhook annotations for requests without violations and use only native validation-failure annotations for generated ValidatingAdmissionPolicies",
	)
)

// GetEmitAdmissionAuditAnnotations returns whether admission audit annotations are enabled.
func GetEmitAdmissionAuditAnnotations() bool {
	return *emitAdmissionAuditAnnotations
}

func GetAdmissionAuditAnnotationsViolationsOnly() bool {
	return *admissionAuditAnnotationsViolationsOnly
}
