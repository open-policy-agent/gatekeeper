package schema

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/utils/ptr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	admissionv1beta1 "k8s.io/api/admissionregistration/v1beta1"
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

func TestHasCELEngine(t *testing.T) {
	tests := []struct {
		name     string
		template *templates.ConstraintTemplate
		expected bool
	}{
		{
			name: "Has CEL engine",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{
							Code: []templates.Code{
								{Engine: Name},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "No CEL engine - different engine",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{
							Code: []templates.Code{
								{Engine: "Rego"},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "No CEL engine - empty code",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{
							Code: []templates.Code{},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "No targets",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{},
				},
			},
			expected: false,
		},
		{
			name: "Multiple targets - invalid",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{Code: []templates.Code{{Engine: Name}}},
						{Code: []templates.Code{{Engine: Name}}},
					},
				},
			},
			expected: false,
		},
		{
			name: "CEL engine with other engines",
			template: &templates.ConstraintTemplate{
				Spec: templates.ConstraintTemplateSpec{
					Targets: []templates.Target{
						{
							Code: []templates.Code{
								{Engine: "Rego"},
								{Engine: Name},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCELEngine(tt.template)
			if got != tt.expected {
				t.Errorf("HasCELEngine() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetFailurePolicy(t *testing.T) {
	originalDefault := *DefaultFailurePolicy
	defer func() {
		*DefaultFailurePolicy = originalDefault
	}()

	tests := []struct {
		name                 string
		source               *Source
		defaultFailurePolicy string
		expected             admissionv1.FailurePolicyType
	}{
		{
			name:                 "Uses specified failure policy Fail",
			source:               &Source{FailurePolicy: ptr.To[string]("Fail")},
			defaultFailurePolicy: "Ignore",
			expected:             admissionv1.Fail,
		},
		{
			name:                 "Uses specified failure policy Ignore",
			source:               &Source{FailurePolicy: ptr.To[string]("Ignore")},
			defaultFailurePolicy: "Fail",
			expected:             admissionv1.Ignore,
		},
		{
			name:                 "Falls back to DefaultFailurePolicy (Fail)",
			source:               &Source{FailurePolicy: nil},
			defaultFailurePolicy: "Fail",
			expected:             admissionv1.Fail,
		},
		{
			name:                 "Falls back to DefaultFailurePolicy (Ignore)",
			source:               &Source{FailurePolicy: nil},
			defaultFailurePolicy: "Ignore",
			expected:             admissionv1.Ignore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*DefaultFailurePolicy = tt.defaultFailurePolicy
			got, err := tt.source.GetFailurePolicy()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || *got != tt.expected {
				t.Errorf("GetFailurePolicy() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetV1Beta1FailurePolicy(t *testing.T) {
	originalDefault := *DefaultFailurePolicy
	defer func() {
		*DefaultFailurePolicy = originalDefault
	}()

	tests := []struct {
		name                 string
		source               *Source
		defaultFailurePolicy string
		expected             admissionv1beta1.FailurePolicyType
	}{
		{
			name:                 "Uses specified failure policy Fail",
			source:               &Source{FailurePolicy: ptr.To[string]("Fail")},
			defaultFailurePolicy: "Ignore",
			expected:             admissionv1beta1.Fail,
		},
		{
			name:                 "Uses specified failure policy Ignore",
			source:               &Source{FailurePolicy: ptr.To[string]("Ignore")},
			defaultFailurePolicy: "Fail",
			expected:             admissionv1beta1.Ignore,
		},
		{
			name:                 "Falls back to DefaultFailurePolicy (Fail)",
			source:               &Source{FailurePolicy: nil},
			defaultFailurePolicy: "Fail",
			expected:             admissionv1beta1.Fail,
		},
		{
			name:                 "Falls back to DefaultFailurePolicy (Ignore)",
			source:               &Source{FailurePolicy: nil},
			defaultFailurePolicy: "Ignore",
			expected:             admissionv1beta1.Ignore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*DefaultFailurePolicy = tt.defaultFailurePolicy
			got, err := tt.source.GetV1Beta1FailurePolicy()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || *got != tt.expected {
				t.Errorf("GetV1Beta1FailurePolicy() = %v, want %v", got, tt.expected)
			}
		})
	}
}
