package webhook

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	admissionAuditAnnotationKey           = "evaluation"
	admissionAuditAnnotationSchemaVersion = "v1"
	admissionAuditEventType               = "validation_admission"
	maxAdmissionAuditAnnotationValueBytes = 10 * 1024
	maxAdmissionAuditDetailsBytes         = 2 * 1024
	maxAdmissionAuditMessageBytes         = 1024
	maxAdmissionAuditCollectedViolations  = 64
)

// admissionAuditAnnotation is a bounded summary of one Gatekeeper validation
// webhook evaluation. The API server audit event already records request
// identity, operation, user information, and response status.
type admissionAuditAnnotation struct {
	SchemaVersion      string                    `json:"schemaVersion"`
	ID                 string                    `json:"id,omitempty"`
	EventType          string                    `json:"eventType"`
	Allowed            bool                      `json:"allowed"`
	ResourceGroup      string                    `json:"resourceGroup,omitempty"`
	ResourceAPIVersion string                    `json:"resourceAPIVersion,omitempty"`
	ResourceKind       string                    `json:"resourceKind,omitempty"`
	ResourceNamespace  string                    `json:"resourceNamespace,omitempty"`
	ResourceName       string                    `json:"resourceName,omitempty"`
	Violations         []admissionAuditViolation `json:"violations"`
	TotalViolations    int                       `json:"totalViolations"`
	IncludedViolations int                       `json:"includedViolations"`
	Truncated          bool                      `json:"truncated"`
}

// admissionAuditViolation intentionally omits resource labels and arbitrary
// Constraint annotations. Both may contain sensitive data and are unbounded.
type admissionAuditViolation struct {
	Details              json.RawMessage `json:"details,omitempty"`
	DetailsOmitted       bool            `json:"detailsOmitted,omitempty"`
	ConstraintGroup      string          `json:"constraintGroup,omitempty"`
	ConstraintAPIVersion string          `json:"constraintAPIVersion,omitempty"`
	ConstraintKind       string          `json:"constraintKind,omitempty"`
	ConstraintName       string          `json:"constraintName,omitempty"`
	ConstraintNamespace  string          `json:"constraintNamespace,omitempty"`
	Message              string          `json:"message,omitempty"`
	MessageTruncated     bool            `json:"messageTruncated,omitempty"`
	EnforcementAction    string          `json:"enforcementAction,omitempty"`
	EnforcementActions   []string        `json:"enforcementActions,omitempty"`
}

type admissionAuditResults struct {
	violations      []admissionAuditViolation
	totalViolations int
}

func (r *admissionAuditResults) add(result *rtypes.Result, actions []string) {
	r.totalViolations++
	if len(r.violations) >= maxAdmissionAuditCollectedViolations {
		return
	}
	r.violations = append(r.violations, newAdmissionAuditViolation(result, actions))
}

func newAdmissionAuditViolation(result *rtypes.Result, actions []string) admissionAuditViolation {
	message, messageTruncated := truncateUTF8(result.Msg, maxAdmissionAuditMessageBytes)
	details, detailsOmitted := boundedAdmissionAuditDetails(result.Metadata)

	violation := admissionAuditViolation{
		Details:              details,
		DetailsOmitted:       detailsOmitted,
		ConstraintGroup:      result.Constraint.GroupVersionKind().Group,
		ConstraintAPIVersion: result.Constraint.GroupVersionKind().Version,
		ConstraintKind:       result.Constraint.GetKind(),
		ConstraintName:       result.Constraint.GetName(),
		ConstraintNamespace:  result.Constraint.GetNamespace(),
		Message:              message,
		MessageTruncated:     messageTruncated,
		EnforcementAction:    result.EnforcementAction,
	}
	if len(actions) > 0 {
		violation.EnforcementActions = uniqueStrings(actions)
	}
	return violation
}

func boundedAdmissionAuditDetails(metadata map[string]interface{}) (json.RawMessage, bool) {
	if metadata == nil {
		return nil, false
	}
	details, found := metadata["details"]
	if !found || details == nil {
		return nil, false
	}

	budget := int64(maxAdmissionAuditDetailsBytes)
	nodes := 4096
	withinBudget, known := consumeAdmissionExportJSONValue(&budget, details, 0, &nodes)
	if !known || !withinBudget {
		return nil, true
	}

	encoded, err := json.Marshal(details)
	if err != nil || len(encoded) > maxAdmissionAuditDetailsBytes {
		return nil, true
	}
	return encoded, false
}

func buildAdmissionAuditAnnotations(req *admission.Request, allowed bool, results admissionAuditResults) (map[string]string, error) {
	annotation := admissionAuditAnnotation{
		SchemaVersion:      admissionAuditAnnotationSchemaVersion,
		ID:                 string(req.UID),
		EventType:          admissionAuditEventType,
		Allowed:            allowed,
		ResourceGroup:      req.Kind.Group,
		ResourceAPIVersion: req.Kind.Version,
		ResourceKind:       req.Kind.Kind,
		ResourceNamespace:  req.Namespace,
		ResourceName:       req.Name,
		Violations:         make([]admissionAuditViolation, 0, len(results.violations)),
		TotalViolations:    results.totalViolations,
	}

	for i := range results.violations {
		annotation.Violations = append(annotation.Violations, results.violations[i])
		annotation.IncludedViolations = len(annotation.Violations)
		encoded, err := json.Marshal(annotation)
		if err != nil {
			return nil, fmt.Errorf("marshal admission audit annotation: %w", err)
		}
		if len(encoded) > maxAdmissionAuditAnnotationValueBytes {
			annotation.Violations = annotation.Violations[:len(annotation.Violations)-1]
			annotation.IncludedViolations = len(annotation.Violations)
			break
		}
	}

	annotation.Truncated = annotation.IncludedViolations < annotation.TotalViolations
	encoded, err := json.Marshal(annotation)
	if err != nil {
		return nil, fmt.Errorf("marshal admission audit annotation: %w", err)
	}
	if len(encoded) > maxAdmissionAuditAnnotationValueBytes {
		return nil, fmt.Errorf("admission audit annotation exceeds %d bytes", maxAdmissionAuditAnnotationValueBytes)
	}

	return map[string]string{admissionAuditAnnotationKey: string(encoded)}, nil
}

func uniqueStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
