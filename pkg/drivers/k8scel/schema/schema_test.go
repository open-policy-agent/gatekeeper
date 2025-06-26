package schema

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	admissionv1beta1 "k8s.io/api/admissionregistration/v1beta1"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
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
	type fields struct {
		Validations        []Validation
		FailurePolicy      *string
		MatchConditions    []MatchCondition
		Variables          []Variable
		GenerateVAP        *bool
		ResourceOperations []string
	}
	type args struct {
		enableDeleteOpsInVwhc *bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []admissionv1beta1.OperationType
		wantErr error
	}{
		{
			name: "Valid ResourceOperations",
			fields: fields{
				ResourceOperations: []string{"CREATE", "UPDATE"},
			},
			args: args{
				enableDeleteOpsInVwhc: ptr.To(true),
			},
			want: []admissionv1beta1.OperationType{
				admissionv1beta1.Create,
				admissionv1beta1.Update,
			},
			wantErr: nil,
		},
		{
			name: "Valid Delete ResourceOperations",
			fields: fields{
				ResourceOperations: []string{"DELETE"},
			},
			args: args{
				enableDeleteOpsInVwhc: ptr.To(true),
			},
			want: []admissionv1beta1.OperationType{
				admissionv1beta1.Delete,
			},
			wantErr: nil,
		},
		{
			name: "OperationAll ResourceOperations",
			fields: fields{
				ResourceOperations: []string{"CREATE", "*"},
			},
			args: args{
				enableDeleteOpsInVwhc: ptr.To(true),
			},
			want: []admissionv1beta1.OperationType{
				admissionv1beta1.OperationAll,
			},
			wantErr: nil,
		},
		{
			name: "InValid ResourceOperations",
			fields: fields{
				ResourceOperations: []string{"CREATE", "Invalid"},
			},
			args: args{
				enableDeleteOpsInVwhc: ptr.To(false),
			},
			want:    nil,
			wantErr: ErrBadResourceOperation,
		},
		{
			name: "Without ResourceOperations and enableDeleteOpsInVwhc",
			fields: fields{
				ResourceOperations: []string{},
			},
			args: args{
				enableDeleteOpsInVwhc: nil,
			},
			want: []admissionv1beta1.OperationType{
				admissionv1beta1.Create,
				admissionv1beta1.Update,
			},
			wantErr: nil,
		},
		{
			name: "Without ResourceOperations but enableDeleteOpsInVwhc",
			fields: fields{
				ResourceOperations: []string{},
			},
			args: args{
				enableDeleteOpsInVwhc: ptr.To(true),
			},
			want: []admissionv1beta1.OperationType{
				admissionv1beta1.Create,
				admissionv1beta1.Update,
				admissionv1beta1.Delete,
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := &Source{
				ResourceOperations: tt.fields.ResourceOperations,
			}
			got, err := in.GetResourceOperations(tt.args.enableDeleteOpsInVwhc)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got %v; wanted %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetResourceOperations() got = %v, want %v", got, tt.want)
			}
		})
	}
}
