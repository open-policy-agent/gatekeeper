package webhook

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type admissionAuditJSONMarshaler struct {
	called *bool
}

func (m admissionAuditJSONMarshaler) MarshalJSON() ([]byte, error) {
	*m.called = true
	return []byte(`{"value":"should not be marshaled"}`), nil
}

func TestBuildAdmissionAuditAnnotations(t *testing.T) {
	annotations, err := buildAdmissionAuditAnnotations(true, admissionAuditResults{})
	require.NoError(t, err)
	require.Len(t, annotations, 1)
	require.JSONEq(t, `{"schemaVersion":"v1","allowed":true,"violations":[],"totalViolations":0,"includedViolations":0,"truncated":false}`, annotations[admissionAuditAnnotationKey])

	var got admissionAuditAnnotation
	require.NoError(t, json.Unmarshal([]byte(annotations[admissionAuditAnnotationKey]), &got))
	require.Equal(t, admissionAuditAnnotationSchemaVersion, got.SchemaVersion)
	require.True(t, got.Allowed)
	require.Empty(t, got.Violations)
	require.Zero(t, got.TotalViolations)
	require.Zero(t, got.IncludedViolations)
	require.False(t, got.Truncated)
}

func TestBuildAdmissionAuditAnnotationsTruncatesAtValueLimit(t *testing.T) {
	violations := make([]admissionAuditViolation, 20)
	for i := range violations {
		violations[i] = admissionAuditViolation{
			ConstraintKind:    "K8sRequiredLabels",
			ConstraintName:    "required-labels",
			Message:           strings.Repeat("x", maxAdmissionAuditMessageBytes),
			EnforcementAction: "deny",
		}
	}

	annotations, err := buildAdmissionAuditAnnotations(false, admissionAuditResults{
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

func TestBoundedAdmissionAuditDetailsInvalidUTF8Limit(tester *testing.T) {
	invalid := strings.Repeat("\xff", 64)
	encodedInvalid, err := json.Marshal(invalid)
	require.NoError(tester, err)
	padding := strings.Repeat("x", maxAdmissionAuditDetailsBytes-len(encodedInvalid))
	for _, testCase := range []struct {
		name    string
		value   string
		omitted bool
	}{
		{name: "invalid prefix at limit", value: invalid + padding},
		{name: "invalid suffix at limit", value: padding + invalid},
		{name: "invalid prefix over limit", value: invalid + padding + "x", omitted: true},
		{name: "invalid suffix over limit", value: padding + invalid + "x", omitted: true},
		{name: "raw input over limit", value: "\xff" + strings.Repeat("x", maxAdmissionAuditDetailsBytes), omitted: true},
	} {
		tester.Run(testCase.name, func(tester *testing.T) {
			encoded, err := json.Marshal(testCase.value)
			require.NoError(tester, err)
			require.Equal(tester, testCase.omitted, len(encoded) > maxAdmissionAuditDetailsBytes)

			budget := int64(maxAdmissionAuditDetailsBytes)
			nodes := 4096
			withinBudget, known := consumeAdmissionExportJSONValue(&budget, testCase.value, 0, &nodes)
			require.True(tester, known)
			require.Equal(tester, !testCase.omitted, withinBudget)

			details, omitted := boundedAdmissionAuditDetails(map[string]interface{}{"details": testCase.value})
			require.Equal(tester, testCase.omitted, omitted)
			if omitted {
				require.Nil(tester, details)
			} else {
				require.Equal(tester, json.RawMessage(encoded), details)
				require.Len(tester, details, maxAdmissionAuditDetailsBytes)
			}
		})
	}
}

func TestAdmissionAuditDetailsPreflightRejectsOversizedValues(tester *testing.T) {
	testCases := []struct {
		name  string
		value interface{}
		count int
	}{
		{name: "large int64 values", value: int64(math.MaxInt64), count: 110},
		{name: "false values", value: false, count: 342},
		{name: "nil containers", value: []interface{}(nil), count: 410},
		{name: "empty containers", value: map[string]interface{}{}, count: 683},
	}

	for _, testCase := range testCases {
		tester.Run(testCase.name, func(tester *testing.T) {
			values := make([]interface{}, testCase.count)
			for index := range values {
				values[index] = testCase.value
			}
			encoded, err := json.Marshal(values)
			require.NoError(tester, err)
			require.Greater(tester, len(encoded), maxAdmissionAuditDetailsBytes)

			budget := int64(maxAdmissionAuditDetailsBytes)
			nodes := 4096
			withinBudget, known := consumeAdmissionExportJSONValue(&budget, values, 0, &nodes)
			require.True(tester, known)
			require.False(tester, withinBudget)

			details, omitted := boundedAdmissionAuditDetails(map[string]interface{}{"details": values})
			require.Nil(tester, details)
			require.True(tester, omitted)
		})
	}
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

func BenchmarkBoundedAdmissionAuditDetailsOversizedInvalidUTF8(benchmark *testing.B) {
	metadata := map[string]interface{}{"details": strings.Repeat("\xff", 16*1024*1024)}
	benchmark.ReportAllocs()

	for benchmark.Loop() {
		boundedAdmissionAuditDetails(metadata)
	}
}

func BenchmarkBoundedAdmissionAuditDetailsOversizedNumbers(benchmark *testing.B) {
	values := make([]interface{}, 110)
	for index := range values {
		values[index] = int64(math.MaxInt64)
	}
	metadata := map[string]interface{}{"details": values}
	benchmark.ReportAllocs()

	for benchmark.Loop() {
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

	annotations, err := buildAdmissionAuditAnnotations(false, results)
	require.NoError(t, err)
	var annotation admissionAuditAnnotation
	require.NoError(t, json.Unmarshal([]byte(annotations[admissionAuditAnnotationKey]), &annotation))
	require.Equal(t, maxAdmissionAuditCollectedViolations+10, annotation.TotalViolations)
	require.True(t, annotation.Truncated)
}
