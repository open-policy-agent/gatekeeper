package util

import "flag"

const (
	// EmitAdmissionAuditAnnotationsFlag is the CLI flag that enables admission audit annotations.
	EmitAdmissionAuditAnnotationsFlag           = "emit-admission-audit-annotations"
	AdmissionAuditAnnotationsIncludeSuccessFlag = "admission-audit-annotations-include-success"
)

var (
	emitAdmissionAuditAnnotations = flag.Bool(
		EmitAdmissionAuditAnnotationsFlag,
		false,
		"(alpha) emit API server audit annotations for validation webhook violations and Gatekeeper-generated ValidatingAdmissionPolicy failures",
	)
	admissionAuditAnnotationsIncludeSuccess = flag.Bool(
		AdmissionAuditAnnotationsIncludeSuccessFlag,
		false,
		"(alpha) when admission audit annotations are enabled, also annotate webhook requests with no violations and add evaluation markers to generated ValidatingAdmissionPolicies",
	)
)

// GetEmitAdmissionAuditAnnotations returns whether admission audit annotations are enabled.
func GetEmitAdmissionAuditAnnotations() bool {
	return *emitAdmissionAuditAnnotations
}

func GetAdmissionAuditAnnotationsIncludeSuccess() bool {
	return *admissionAuditAnnotationsIncludeSuccess
}
