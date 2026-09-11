package util

import "flag"

const (
	// EmitAdmissionAuditAnnotationsFlag is the CLI flag that enables admission audit annotations.
	EmitAdmissionAuditAnnotationsFlag = "emit-admission-audit-annotations"
)

var emitAdmissionAuditAnnotations = flag.Bool(
	EmitAdmissionAuditAnnotationsFlag,
	false,
	"(alpha) emit API server audit annotations for evaluated validation requests and Gatekeeper-generated ValidatingAdmissionPolicy evaluations",
)

// GetEmitAdmissionAuditAnnotations returns whether admission audit annotations are enabled.
func GetEmitAdmissionAuditAnnotations() bool {
	return *emitAdmissionAuditAnnotations
}
