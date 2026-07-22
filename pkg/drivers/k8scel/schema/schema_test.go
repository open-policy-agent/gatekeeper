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

func TestFailurePolicyForK8sNativeValidation(t *testing.T) {
	original := *DefaultFailurePolicyForK8sNativeValidation
	t.Cleanup(func() { *DefaultFailurePolicyForK8sNativeValidation = original })

	tests := []struct {
		name                 string
		defaultFailurePolicy string
		sourceFailurePolicy  *string
		want                 admissionv1.FailurePolicyType
	}{
		{
			name:                 "omitted policy uses Fail default",
			defaultFailurePolicy: string(admissionv1.Fail),
			want:                 admissionv1.Fail,
		},
		{
			name:                 "omitted policy uses Ignore default",
			defaultFailurePolicy: string(admissionv1.Ignore),
			want:                 admissionv1.Ignore,
		},
		{
			name:                 "explicit Fail overrides Ignore default",
			defaultFailurePolicy: string(admissionv1.Ignore),
			sourceFailurePolicy:  ptr.To(string(admissionv1.Fail)),
			want:                 admissionv1.Fail,
		},
		{
			name:                 "explicit Ignore overrides Fail default",
			defaultFailurePolicy: string(admissionv1.Fail),
			sourceFailurePolicy:  ptr.To(string(admissionv1.Ignore)),
			want:                 admissionv1.Ignore,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			*DefaultFailurePolicyForK8sNativeValidation = test.defaultFailurePolicy
			source := &Source{FailurePolicy: test.sourceFailurePolicy}

			failurePolicy, err := source.GetFailurePolicy()
			if err != nil {
				t.Fatalf("GetFailurePolicy() returned an unexpected error: %v", err)
			}
			if failurePolicy == nil || *failurePolicy != test.want {
				t.Fatalf("GetFailurePolicy() = %v, want %s", failurePolicy, test.want)
			}

			v1beta1FailurePolicy, err := source.GetV1Beta1FailurePolicy()
			if err != nil {
				t.Fatalf("GetV1Beta1FailurePolicy() returned an unexpected error: %v", err)
			}
			wantV1Beta1 := admissionv1beta1.FailurePolicyType(test.want)
			if v1beta1FailurePolicy == nil || *v1beta1FailurePolicy != wantV1Beta1 {
				t.Fatalf("GetV1Beta1FailurePolicy() = %v, want %s", v1beta1FailurePolicy, wantV1Beta1)
			}
		})
	}
}

func TestDefaultFailurePolicyForK8sNativeValidationIsValidatedAtStartup(t *testing.T) {
	original := *DefaultFailurePolicyForK8sNativeValidation
	t.Cleanup(func() { *DefaultFailurePolicyForK8sNativeValidation = original })

	*DefaultFailurePolicyForK8sNativeValidation = "Unsupported"

	if err := ValidateDefaultFailurePolicyForK8sNativeValidation(); !errors.Is(err, ErrBadFailurePolicy) {
		t.Fatalf("ValidateDefaultFailurePolicyForK8sNativeValidation() error = %v, want %v", err, ErrBadFailurePolicy)
	}
}

func TestSetDefaultFailurePolicyForK8sNativeValidation(t *testing.T) {
	original := *DefaultFailurePolicyForK8sNativeValidation
	t.Cleanup(func() { *DefaultFailurePolicyForK8sNativeValidation = original })

	if err := SetDefaultFailurePolicyForK8sNativeValidation(string(admissionv1.Ignore)); err != nil {
		t.Fatalf("SetDefaultFailurePolicyForK8sNativeValidation() returned an unexpected error: %v", err)
	}
	if *DefaultFailurePolicyForK8sNativeValidation != string(admissionv1.Ignore) {
		t.Fatalf("DefaultFailurePolicyForK8sNativeValidation = %s, want %s", *DefaultFailurePolicyForK8sNativeValidation, admissionv1.Ignore)
	}

	if err := SetDefaultFailurePolicyForK8sNativeValidation("Unsupported"); !errors.Is(err, ErrBadFailurePolicy) {
		t.Fatalf("SetDefaultFailurePolicyForK8sNativeValidation() error = %v, want %v", err, ErrBadFailurePolicy)
	}
	if *DefaultFailurePolicyForK8sNativeValidation != string(admissionv1.Ignore) {
		t.Fatalf("DefaultFailurePolicyForK8sNativeValidation changed after invalid input: got %s, want %s", *DefaultFailurePolicyForK8sNativeValidation, admissionv1.Ignore)
	}
}

func TestSourceValidateDoesNotValidateDefaultFailurePolicyForK8sNativeValidation(t *testing.T) {
	original := *DefaultFailurePolicyForK8sNativeValidation
	t.Cleanup(func() { *DefaultFailurePolicyForK8sNativeValidation = original })

	*DefaultFailurePolicyForK8sNativeValidation = "Unsupported"

	if err := (&Source{}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
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
