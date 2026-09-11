package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type admissionAuditJSONMarshaler struct {
	called *bool
}

func (m admissionAuditJSONMarshaler) MarshalJSON() ([]byte, error) {
	*m.called = true
	return []byte(`{"value":"should not be marshaled"}`), nil
}

func TestBuildAdmissionAuditAnnotations(t *testing.T) {
	req := &admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:       types.UID("request-1"),
		Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		Namespace: "default",
		Name:      "example",
	}}

	annotations, err := buildAdmissionAuditAnnotations(req, true, admissionAuditResults{})
	require.NoError(t, err)
	require.Len(t, annotations, 1)

	var got admissionAuditAnnotation
	require.NoError(t, json.Unmarshal([]byte(annotations[admissionAuditAnnotationKey]), &got))
	require.Equal(t, admissionAuditAnnotationSchemaVersion, got.SchemaVersion)
	require.Equal(t, "request-1", got.ID)
	require.Equal(t, admissionAuditEventType, got.EventType)
	require.True(t, got.Allowed)
	require.Equal(t, "apps", got.ResourceGroup)
	require.Equal(t, "v1", got.ResourceAPIVersion)
	require.Equal(t, "Deployment", got.ResourceKind)
	require.Equal(t, "default", got.ResourceNamespace)
	require.Equal(t, "example", got.ResourceName)
	require.Empty(t, got.Violations)
	require.Zero(t, got.TotalViolations)
	require.Zero(t, got.IncludedViolations)
	require.False(t, got.Truncated)
}

func TestBuildAdmissionAuditAnnotationsTruncatesAtValueLimit(t *testing.T) {
	req := &admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
	}}
	violations := make([]admissionAuditViolation, 20)
	for i := range violations {
		violations[i] = admissionAuditViolation{
			ConstraintKind:    "K8sRequiredLabels",
			ConstraintName:    "required-labels",
			Message:           strings.Repeat("x", maxAdmissionAuditMessageBytes),
			EnforcementAction: "deny",
		}
	}

	annotations, err := buildAdmissionAuditAnnotations(req, false, admissionAuditResults{
		violations:      violations,
		totalViolations: len(violations),
	})
	require.NoError(t, err)
	value := annotations[admissionAuditAnnotationKey]
	require.LessOrEqual(t, len(value), maxAdmissionAuditAnnotationValueBytes)

	var got admissionAuditAnnotation
	require.NoError(t, json.Unmarshal([]byte(value), &got))
	require.False(t, got.Allowed)
	require.Equal(t, len(violations), got.TotalViolations)
	require.Less(t, got.IncludedViolations, got.TotalViolations)
	require.Len(t, got.Violations, got.IncludedViolations)
	require.True(t, got.Truncated)
}

func TestNewAdmissionAuditViolationBoundsUserControlledFields(t *testing.T) {
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "K8sRequiredLabels",
	})
	constraint.SetName("required-labels")
	result := &rtypes.Result{
		Constraint:        constraint,
		Msg:               strings.Repeat("界", maxAdmissionAuditMessageBytes),
		Metadata:          map[string]interface{}{"details": map[string]interface{}{"value": strings.Repeat("x", maxAdmissionAuditDetailsBytes)}},
		EnforcementAction: "scoped",
	}

	got := newAdmissionAuditViolation(result, []string{"deny", "deny", "warn"})
	require.LessOrEqual(t, len(got.Message), maxAdmissionAuditMessageBytes)
	require.True(t, utf8.ValidString(got.Message))
	require.True(t, got.MessageTruncated)
	require.Nil(t, got.Details)
	require.True(t, got.DetailsOmitted)
	require.Equal(t, []string{"deny", "warn"}, got.EnforcementActions)
}

func TestBoundedAdmissionAuditDetailsRejectsBeforeMarshal(t *testing.T) {
	t.Run("bounded policy JSON", func(t *testing.T) {
		details, omitted := boundedAdmissionAuditDetails(map[string]interface{}{
			"details": map[string]interface{}{
				"count":  json.Number("2"),
				"labels": []interface{}{"owner", "team"},
			},
		})

		require.JSONEq(t, `{"count":2,"labels":["owner","team"]}`, string(details))
		require.False(t, omitted)
	})

	t.Run("oversized JSON string", func(t *testing.T) {
		details, omitted := boundedAdmissionAuditDetails(map[string]interface{}{
			"details": strings.Repeat("x", maxAdmissionAuditDetailsBytes+1),
		})

		require.Nil(t, details)
		require.True(t, omitted)
	})

	t.Run("unknown JSON marshaler", func(t *testing.T) {
		called := false
		details, omitted := boundedAdmissionAuditDetails(map[string]interface{}{
			"details": admissionAuditJSONMarshaler{called: &called},
		})

		require.Nil(t, details)
		require.True(t, omitted)
		require.False(t, called)
	})
}

func BenchmarkBoundedAdmissionAuditDetailsOversized(b *testing.B) {
	metadata := map[string]interface{}{
		"details": strings.Repeat("x", 16*1024*1024),
	}
	b.ReportAllocs()
	b.SetBytes(16 * 1024 * 1024)

	for b.Loop() {
		boundedAdmissionAuditDetails(metadata)
	}
}

func TestAdmissionAuditViolationUsesExplicitConstraintFieldNames(t *testing.T) {
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "K8sRequiredLabels",
	})
	constraint.SetNamespace("policy-system")
	constraint.SetName("required-labels")

	encoded, err := json.Marshal(newAdmissionAuditViolation(&rtypes.Result{Constraint: constraint}, nil))
	require.NoError(t, err)

	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Equal(t, "constraints.gatekeeper.sh", fields["constraintGroup"])
	require.Equal(t, "v1beta1", fields["constraintAPIVersion"])
	require.Equal(t, "K8sRequiredLabels", fields["constraintKind"])
	require.Equal(t, "required-labels", fields["constraintName"])
	require.Equal(t, "policy-system", fields["constraintNamespace"])
	for _, legacyField := range []string{"group", "version", "kind", "name", "namespace"} {
		require.NotContains(t, fields, legacyField)
	}
}

func TestAdmissionAuditResultsBoundsCollectedViolations(t *testing.T) {
	constraint := &unstructured.Unstructured{}
	constraint.SetName("constraint")
	result := &rtypes.Result{Constraint: constraint, EnforcementAction: "deny"}
	var results admissionAuditResults
	for i := 0; i < maxAdmissionAuditCollectedViolations+10; i++ {
		results.add(result, nil)
	}

	require.Equal(t, maxAdmissionAuditCollectedViolations+10, results.totalViolations)
	require.Len(t, results.violations, maxAdmissionAuditCollectedViolations)

	annotations, err := buildAdmissionAuditAnnotations(&admission.Request{}, false, results)
	require.NoError(t, err)
	var annotation admissionAuditAnnotation
	require.NoError(t, json.Unmarshal([]byte(annotations[admissionAuditAnnotationKey]), &annotation))
	require.Equal(t, maxAdmissionAuditCollectedViolations+10, annotation.TotalViolations)
	require.True(t, annotation.Truncated)
}
