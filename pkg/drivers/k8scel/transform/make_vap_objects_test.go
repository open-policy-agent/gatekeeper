package transform

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	rschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

func TestTemplateToPolicyDefinition(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		source      *schema.Source
		expectedErr error
		expected    *admissionregistrationv1beta1.ValidatingAdmissionPolicy
	}{
		{
			name: "Valid Template",
			kind: "SomePolicy",
			source: &schema.Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []schema.MatchCondition{
					{
						Name:       "must_match_something",
						Expression: "true == true",
					},
				},
				Variables: []schema.Variable{
					{
						Name:       "my_variable",
						Expression: "true",
					},
				},
				Validations: []schema.Validation{
					{
						Expression:        "1 == 1",
						Message:           "some fallback message",
						MessageExpression: `"some CEL string"`,
					},
				},
			},
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-somepolicy",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
					ParamKind: &admissionregistrationv1beta1.ParamKind{
						APIVersion: "constraints.gatekeeper.sh/v1beta1",
						Kind:       "SomePolicy",
					},
					MatchConstraints: &admissionregistrationv1beta1.MatchResources{
						ResourceRules: []admissionregistrationv1beta1.NamedRuleWithOperations{
							{
								RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
									Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
									Rule:       admissionregistrationv1beta1.Rule{APIGroups: []string{"*"}, APIVersions: []string{"*"}, Resources: []string{"*"}},
								},
							},
						},
					},
					MatchConditions: []admissionregistrationv1beta1.MatchCondition{
						{
							Name:       "must_match_something",
							Expression: "true == true",
						},
						{
							Name:       "gatekeeper_internal_match_excluded_namespaces",
							Expression: matchExcludedNamespacesGlob,
						},
						{
							Name:       "gatekeeper_internal_match_namespaces",
							Expression: matchNamespacesGlob,
						},
						{
							Name:       "gatekeeper_internal_match_name",
							Expression: matchNameGlob,
						},
						{
							Name:       "gatekeeper_internal_match_kinds",
							Expression: matchKinds,
						},
					},
					Validations: []admissionregistrationv1beta1.Validation{
						{
							Expression:        "1 == 1",
							Message:           "some fallback message",
							MessageExpression: `"some CEL string"`,
						},
					},
					FailurePolicy: ptr.To[admissionregistrationv1beta1.FailurePolicyType](admissionregistrationv1beta1.Fail),
					Variables: []admissionregistrationv1beta1.Variable{
						{
							Name:       schema.ObjectName,
							Expression: `has(request.operation) && request.operation == "DELETE" && object == null ? oldObject : object`,
						},
						{
							Name:       schema.ParamsName,
							Expression: "!has(params.spec) ? null : !has(params.spec.parameters) ? null: params.spec.parameters",
						},
						{
							Name:       "my_variable",
							Expression: "true",
						},
					},
				},
			},
		},
		{
			name: "Invalid Match Condition",
			kind: "SomePolicy",
			source: &schema.Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []schema.MatchCondition{
					{
						Name:       "gatekeeper_internal_match_something",
						Expression: "true == true",
					},
				},
				Variables: []schema.Variable{
					{
						Name:       "my_variable",
						Expression: "true",
					},
				},
				Validations: []schema.Validation{
					{
						Expression:        "1 == 1",
						Message:           "some fallback message",
						MessageExpression: `"some CEL string"`,
					},
				},
			},
			expectedErr: schema.ErrBadMatchCondition,
		},
		{
			name: "Invalid Variable",
			kind: "SomePolicy",
			source: &schema.Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []schema.MatchCondition{
					{
						Name:       "match_something",
						Expression: "true == true",
					},
				},
				Variables: []schema.Variable{
					{
						Name:       "gatekeeper_internal_my_variable",
						Expression: "true",
					},
				},
				Validations: []schema.Validation{
					{
						Expression:        "1 == 1",
						Message:           "some fallback message",
						MessageExpression: `"some CEL string"`,
					},
				},
			},
			expectedErr: schema.ErrBadVariable,
		},
		{
			name: "No Clobbering Params",
			kind: "SomePolicy",
			source: &schema.Source{
				FailurePolicy: ptr.To[string]("Fail"),
				MatchConditions: []schema.MatchCondition{
					{
						Name:       "match_something",
						Expression: "true == true",
					},
				},
				Variables: []schema.Variable{
					{
						Name:       "params",
						Expression: "true",
					},
				},
				Validations: []schema.Validation{
					{
						Expression:        "1 == 1",
						Message:           "some fallback message",
						MessageExpression: `"some CEL string"`,
					},
				},
			},
			expectedErr: schema.ErrBadVariable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rawSrc := test.source.MustToUnstructured()

			template := &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.ToLower(test.kind),
				},
				Spec: templates.ConstraintTemplateSpec{
					CRD: templates.CRD{
						Spec: templates.CRDSpec{
							Names: templates.Names{
								Kind: test.kind,
							},
						},
					},
					Targets: []templates.Target{
						{
							Code: []templates.Code{
								{
									Engine: schema.Name,
									Source: &templates.Anything{
										Value: rawSrc,
									},
								},
							},
						},
					},
				},
			}

			obj, err := TemplateToPolicyDefinition(template)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("unexpected error. got %v; wanted %v", err, test.expectedErr)
			}
			if !reflect.DeepEqual(obj, test.expected) {
				t.Errorf("got %+v\n\nwant %+v", *obj, *test.expected)
			}
		})
	}
}

func newTestConstraint(enforcementAction string, namespaceSelector, labelSelector *metav1.LabelSelector, constraint *unstructured.Unstructured) *unstructured.Unstructured {
	constraint.SetGroupVersionKind(rschema.GroupVersionKind{Group: constraints.Group, Version: "v1beta1", Kind: "FooTemplate"})
	constraint.SetName("foo-name")
	if namespaceSelector != nil {
		nss, err := runtime.DefaultUnstructuredConverter.ToUnstructured(namespaceSelector)
		if err != nil {
			panic(fmt.Errorf("%w: could not convert namespace selector", err))
		}
		if err := unstructured.SetNestedMap(constraint.Object, nss, "spec", "match", "namespaceSelector"); err != nil {
			panic(fmt.Errorf("%w: could not set namespace selector", err))
		}
	}
	if labelSelector != nil {
		ls, err := runtime.DefaultUnstructuredConverter.ToUnstructured(labelSelector)
		if err != nil {
			panic(fmt.Errorf("%w: could not convert label selector", err))
		}
		if err := unstructured.SetNestedMap(constraint.Object, ls, "spec", "match", "labelSelector"); err != nil {
			panic(fmt.Errorf("%w: could not set label selector", err))
		}
	}
	if enforcementAction != "" {
		if err := unstructured.SetNestedField(constraint.Object, enforcementAction, "spec", "enforcementAction"); err != nil {
			panic(fmt.Errorf("%w: could not set enforcement action", err))
		}
	}
	return constraint
}

func TestConstraintToBinding(t *testing.T) {
	tests := []struct {
		name               string
		constraint         *unstructured.Unstructured
		enforcementActions []string
		expectedErr        error
		expected           *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding
	}{
		{
			name:               "empty constraint",
			constraint:         newTestConstraint("", nil, nil, &unstructured.Unstructured{}),
			enforcementActions: []string{"deny"},
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources:    &admissionregistrationv1beta1.MatchResources{},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
				},
			},
		},
		{
			name:               "with object selector",
			constraint:         newTestConstraint("", nil, &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}}, &unstructured.Unstructured{}),
			enforcementActions: []string{"deny"},
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources: &admissionregistrationv1beta1.MatchResources{
						ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}},
					},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
				},
			},
		},
		{
			name:               "with namespace selector",
			enforcementActions: []string{"deny"},
			constraint:         newTestConstraint("", &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}}, nil, &unstructured.Unstructured{}),
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources: &admissionregistrationv1beta1.MatchResources{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}},
					},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
				},
			},
		},
		{
			name:               "with both selectors",
			enforcementActions: []string{"deny"},
			constraint:         newTestConstraint("", &metav1.LabelSelector{MatchLabels: map[string]string{"matchNS": "yes"}}, &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}}, &unstructured.Unstructured{}),
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources: &admissionregistrationv1beta1.MatchResources{
						ObjectSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"match": "yes"}},
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"matchNS": "yes"}},
					},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
				},
			},
		},
		{
			name:               "with explicit deny",
			enforcementActions: []string{"deny"},
			constraint:         newTestConstraint("deny", nil, nil, &unstructured.Unstructured{}),
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources:    &admissionregistrationv1beta1.MatchResources{},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
				},
			},
		},
		{
			name:               "with warn",
			enforcementActions: []string{"warn"},
			constraint:         newTestConstraint("warn", nil, nil, &unstructured.Unstructured{}),
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources:    &admissionregistrationv1beta1.MatchResources{},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Warn},
				},
			},
		},
		{
			name:               "with dryrun",
			enforcementActions: []string{"dryrun"},
			constraint:         newTestConstraint("dryrun", nil, nil, &unstructured.Unstructured{}),
			expected: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-foo-name",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "gatekeeper-footemplate",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name:                    "foo-name",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
					},
					MatchResources:    &admissionregistrationv1beta1.MatchResources{},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Audit},
				},
			},
		},
		{
			name:               "unrecognized enforcement action",
			enforcementActions: []string{"magicunicorns"},
			constraint:         newTestConstraint("magicunicorns", nil, nil, &unstructured.Unstructured{}),
			expected:           nil,
			expectedErr:        ErrBadEnforcementAction,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := ConstraintToBinding(test.constraint, test.enforcementActions)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("unexpected error. got %v; wanted %v", err, test.expectedErr)
			}
			if !reflect.DeepEqual(binding, test.expected) {
				t.Errorf("got %+v\n\nwant %+v", *binding, *test.expected)
			}
		})
	}
}
