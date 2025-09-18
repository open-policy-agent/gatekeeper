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

func TestSource_GetResourceOperations(t *testing.T) {
	truePtr := func() *bool { b := true; return &b }()
	falsePtr := func() *bool { b := false; return &b }()

	tests := []struct {
		name        string
		source      *Source
		opsInVwhc   OpsInVwhc
		expectedOps []admissionv1.OperationType
		expectError bool
	}{
		{
			name:        "empty resource operations",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{}},
			opsInVwhc:   OpsInVwhc{},
			expectedOps: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update},
			expectError: false,
		},
		{
			name:        "create operation",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Create}},
			opsInVwhc:   OpsInVwhc{},
			expectedOps: []admissionv1.OperationType{admissionv1.Create},
			expectError: false,
		},
		{
			name:        "update operation",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Update}},
			opsInVwhc:   OpsInVwhc{},
			expectedOps: []admissionv1.OperationType{admissionv1.Update},
			expectError: false,
		},
		{
			name:        "delete operation disabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Delete}},
			opsInVwhc:   OpsInVwhc{EnableDeleteOpsInVwhc: falsePtr},
			expectedOps: []admissionv1.OperationType{},
			expectError: false,
		},
		{
			name:        "delete operation enabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Delete}},
			opsInVwhc:   OpsInVwhc{EnableDeleteOpsInVwhc: truePtr},
			expectedOps: []admissionv1.OperationType{admissionv1.Delete},
			expectError: false,
		},
		{
			name:        "connect operation disabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Connect}},
			opsInVwhc:   OpsInVwhc{EnableConectOpsInVwhc: falsePtr},
			expectedOps: []admissionv1.OperationType{},
			expectError: false,
		},
		{
			name:        "connect operation enabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Connect}},
			opsInVwhc:   OpsInVwhc{EnableConectOpsInVwhc: truePtr},
			expectedOps: []admissionv1.OperationType{admissionv1.Connect},
			expectError: false,
		},
		{
			name:        "operation all with both enabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.OperationAll}},
			opsInVwhc:   OpsInVwhc{EnableDeleteOpsInVwhc: truePtr, EnableConectOpsInVwhc: truePtr},
			expectedOps: []admissionv1.OperationType{admissionv1.OperationAll},
			expectError: false,
		},
		{
			name:        "operation all with delete disabled",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.OperationAll}},
			opsInVwhc:   OpsInVwhc{EnableDeleteOpsInVwhc: falsePtr, EnableConectOpsInVwhc: truePtr},
			expectedOps: []admissionv1.OperationType{},
			expectError: false,
		},
		{
			name:        "unknown operation",
			source:      &Source{ResourceOperations: []admissionv1.OperationType{"Unknown"}},
			opsInVwhc:   OpsInVwhc{},
			expectedOps: nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.source.GetResourceOperations(tt.opsInVwhc)

			if (err != nil) != tt.expectError {
				t.Errorf("GetResourceOperations() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if !sets.NewString(stringSliceFromOps(got)...).Equal(sets.NewString(stringSliceFromOps(tt.expectedOps)...)) {
				t.Errorf("GetResourceOperations() = %v, want %v", got, tt.expectedOps)
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

func TestSource_GetResourceOperationsWhenVwhcChange(t *testing.T) {
	truePtr := func() *bool { b := true; return &b }()
	falsePtr := func() *bool { b := false; return &b }()

	tests := []struct {
		name           string
		source         *Source
		deleteChanged  bool
		connectChanged bool
		vwhcOps        OpsInVwhc
		vapOps         []admissionv1.OperationType
		expectedOps    []admissionv1.OperationType
	}{
		{
			name:           "delete changed and enabled with source support",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Delete}},
			deleteChanged:  true,
			connectChanged: false,
			vwhcOps:        OpsInVwhc{EnableDeleteOpsInVwhc: truePtr},
			vapOps:         []admissionv1.OperationType{},
			expectedOps:    []admissionv1.OperationType{admissionv1.Delete},
		},
		{
			name:           "delete changed and disabled",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Delete}},
			deleteChanged:  true,
			connectChanged: false,
			vwhcOps:        OpsInVwhc{EnableDeleteOpsInVwhc: falsePtr},
			vapOps:         []admissionv1.OperationType{admissionv1.Delete},
			expectedOps:    []admissionv1.OperationType{},
		},
		{
			name:           "connect changed and enabled with source support",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Connect}},
			deleteChanged:  false,
			connectChanged: true,
			vwhcOps:        OpsInVwhc{EnableConectOpsInVwhc: truePtr},
			vapOps:         []admissionv1.OperationType{},
			expectedOps:    []admissionv1.OperationType{admissionv1.Connect},
		},
		{
			name:           "connect changed and disabled",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Connect}},
			deleteChanged:  false,
			connectChanged: true,
			vwhcOps:        OpsInVwhc{EnableConectOpsInVwhc: falsePtr},
			vapOps:         []admissionv1.OperationType{admissionv1.Connect},
			expectedOps:    []admissionv1.OperationType{},
		},
		{
			name:           "delete enabled but source does not support",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Create}},
			deleteChanged:  true,
			connectChanged: false,
			vwhcOps:        OpsInVwhc{EnableDeleteOpsInVwhc: truePtr},
			vapOps:         []admissionv1.OperationType{},
			expectedOps:    []admissionv1.OperationType{},
		},
		{
			name:           "connect enabled but source does not support",
			source:         &Source{ResourceOperations: []admissionv1.OperationType{admissionv1.Create}},
			deleteChanged:  false,
			connectChanged: true,
			vwhcOps:        OpsInVwhc{EnableConectOpsInVwhc: truePtr},
			vapOps:         []admissionv1.OperationType{},
			expectedOps:    []admissionv1.OperationType{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.source.GetResourceOperationsWhenVwhcChange(
				tt.deleteChanged,
				tt.connectChanged,
				tt.vwhcOps,
				tt.vapOps,
			)

			if !sets.NewString(stringSliceFromOps(got)...).Equal(sets.NewString(stringSliceFromOps(tt.expectedOps)...)) {
				t.Errorf("GetResourceOperationsWhenVwhcChange() = %v, want %v", got, tt.expectedOps)
			}
		})
	}
}
