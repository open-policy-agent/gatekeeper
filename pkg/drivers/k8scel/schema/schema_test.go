package schema

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
)

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		source      *Source
		expectedErr error
	}{
		{
			name: "Valid Template",
			source: &Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "my_variable",
						Expression: "true",
					},
				},
			},
		},
		{
			name: "Bad Failure Policy",
			source: &Source{
				FailurePolicy: ptr.To[string]("Unsupported"),
				MatchConditions: []MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "my_variable",
						Expression: "true",
					},
				},
			},
			expectedErr: ErrBadFailurePolicy,
		},
		{
			name: "Reserved Match Condition",
			source: &Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []MatchCondition{
					{
						Name:       "gatekeeper_internal_must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "my_variable",
						Expression: "true",
					},
				},
			},
			expectedErr: ErrBadMatchCondition,
		},
		{
			name: "Reserved Variable Prefix",
			source: &Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "gatekeeper_internal_my_variable",
						Expression: "true",
					},
				},
			},
			expectedErr: ErrBadVariable,
		},
		{
			name: "Reserved Variable `Params`",
			source: &Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "params",
						Expression: "true",
					},
				},
			},
			expectedErr: ErrBadVariable,
		},
		{
			name: "Reserved Variable `anyObject`",
			source: &Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []Variable{
					{
						Name:       "anyObject",
						Expression: "true",
					},
				},
			},
			expectedErr: ErrBadVariable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.source.Validate()
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v; wanted %v", err, test.expectedErr)
			}
		})
	}

	// ensure that GetSource() runs validation. Do not change this behavior.
	for _, test := range tests {
		t.Run(fmt.Sprintf("%v for GetSurce", test.name), func(t *testing.T) {
			rawSrc := test.source.MustToUnstructured()
			code := templates.Code{
				Engine: Name,
				Source: &templates.Anything{
					Value: rawSrc,
				},
			}
			_, err := GetSource(code)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v; wanted %v", err, test.expectedErr)
			}
		})
	}

	// ensure that GetSourceFromTemplate() runs validation. Do not change this behavior.
	for _, test := range tests {
		t.Run(fmt.Sprintf("%v for GetSourceFromTemplate", test.name), func(t *testing.T) {
			rawSrc := test.source.MustToUnstructured()
			code := templates.Code{
				Engine: Name,
				Source: &templates.Anything{
					Value: rawSrc,
				},
			}
			template := &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{
							Code: []templates.Code{code},
						},
					},
				},
			}
			_, err := GetSourceFromTemplate(template)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("got %v; wanted %v", err, test.expectedErr)
			}
		})
	}
}

func TestSource_GetIntersectOperations(t *testing.T) {
	tests := []struct {
		name        string
		source      *Source
		opsCache    *WebhookOperationsCache
		expectedOps []admissionv1.OperationType
		expectError bool
	}{
		{
			name:   "empty operations with webhook ops",
			source: &Source{Operations: []admissionv1.OperationType{}},
			opsCache: func() *WebhookOperationsCache {
				cache := NewWebhookOperationsCache()
				cache.operations[admissionv1.Create] = true
				cache.operations[admissionv1.Update] = true
				return cache
			}(),
			expectedOps: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update},
			expectError: false,
		},
		{
			name:   "create operation",
			source: &Source{Operations: []admissionv1.OperationType{admissionv1.Create}},
			opsCache: func() *WebhookOperationsCache {
				cache := NewWebhookOperationsCache()
				cache.operations[admissionv1.Create] = true
				cache.operations[admissionv1.Update] = true
				return cache
			}(),
			expectedOps: []admissionv1.OperationType{admissionv1.Create},
			expectError: false,
		},
		{
			name:   "delete operation not in webhook",
			source: &Source{Operations: []admissionv1.OperationType{admissionv1.Delete}},
			opsCache: func() *WebhookOperationsCache {
				cache := NewWebhookOperationsCache()
				cache.operations[admissionv1.Create] = true
				return cache
			}(),
			expectedOps: nil,
			expectError: true,
		},
		{
			name:   "delete operation in webhook",
			source: &Source{Operations: []admissionv1.OperationType{admissionv1.Delete}},
			opsCache: func() *WebhookOperationsCache {
				cache := NewWebhookOperationsCache()
				cache.operations[admissionv1.Delete] = true
				return cache
			}(),
			expectedOps: []admissionv1.OperationType{admissionv1.Delete},
			expectError: false,
		},
		{
			name:   "operation all",
			source: &Source{Operations: []admissionv1.OperationType{admissionv1.OperationAll}},
			opsCache: func() *WebhookOperationsCache {
				cache := NewWebhookOperationsCache()
				cache.operations[admissionv1.OperationAll] = true
				return cache
			}(),
			expectedOps: []admissionv1.OperationType{admissionv1.OperationAll},
			expectError: false,
		},
		{
			name:        "empty cache and empty source",
			source:      &Source{Operations: []admissionv1.OperationType{}},
			opsCache:    NewWebhookOperationsCache(),
			expectedOps: nil,
			expectError: true,
		},
		{
			name:        "unknown operation",
			source:      &Source{Operations: []admissionv1.OperationType{"Unknown"}},
			opsCache:    NewWebhookOperationsCache(),
			expectedOps: nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.source.GetIntersectOperations(tt.opsCache)

			if (err != nil) != tt.expectError {
				t.Errorf("GetIntersectOperations() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if !sets.NewString(stringSliceFromOps(got)...).Equal(sets.NewString(stringSliceFromOps(tt.expectedOps)...)) {
				t.Errorf("GetIntersectOperations() = %v, want %v", got, tt.expectedOps)
			}
		})
	}
}

// Helper function to convert v1 operation types to strings for comparison
func stringSliceFromOps(ops []admissionv1.OperationType) []string {
	result := make([]string, len(ops))
	for i, op := range ops {
		result[i] = string(op)
	}
	return result
}
