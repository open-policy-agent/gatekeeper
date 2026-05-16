package schema

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	admissionv1beta1 "k8s.io/api/admissionregistration/v1beta1"
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

func TestDefaultFailurePolicy(t *testing.T) {
	original := *DefaultFailurePolicy
	t.Cleanup(func() { *DefaultFailurePolicy = original })

	source := &Source{}
	*DefaultFailurePolicy = string(admissionv1.Ignore)

	failurePolicy, err := source.GetFailurePolicy()
	if err != nil {
		t.Fatalf("GetFailurePolicy() returned an unexpected error: %v", err)
	}
	if failurePolicy == nil || *failurePolicy != admissionv1.Ignore {
		t.Fatalf("GetFailurePolicy() = %v, want %s", failurePolicy, admissionv1.Ignore)
	}

	v1beta1FailurePolicy, err := source.GetV1Beta1FailurePolicy()
	if err != nil {
		t.Fatalf("GetV1Beta1FailurePolicy() returned an unexpected error: %v", err)
	}
	if v1beta1FailurePolicy == nil || *v1beta1FailurePolicy != admissionv1beta1.Ignore {
		t.Fatalf("GetV1Beta1FailurePolicy() = %v, want %s", v1beta1FailurePolicy, admissionv1beta1.Ignore)
	}
}

func TestDefaultFailurePolicyIsValidated(t *testing.T) {
	original := *DefaultFailurePolicy
	t.Cleanup(func() { *DefaultFailurePolicy = original })

	source := &Source{}
	*DefaultFailurePolicy = "Unsupported"

	if err := source.Validate(); !errors.Is(err, ErrBadFailurePolicy) {
		t.Fatalf("Validate() error = %v, want %v", err, ErrBadFailurePolicy)
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
