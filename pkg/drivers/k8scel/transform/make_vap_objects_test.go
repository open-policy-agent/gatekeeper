package transform

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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

func TestTemplateToPolicyDefinitionWithWebhookConfig(t *testing.T) {
	baseSource := &schema.Source{
		FailurePolicy: ptr.To[string]("Fail"),
		MatchConditions: []schema.MatchCondition{
			{
				Name:       "template_condition",
				Expression: "true",
			},
		},
		Variables: []schema.Variable{
			{
				Name:       "my_var",
				Expression: "true",
			},
		},
		Validations: []schema.Validation{
			{
				Expression:        "variables.my_var == true",
				Message:           "validation failed",
				MessageExpression: `"validation message"`,
			},
		},
	}

	tests := []struct {
		name                     string
		kind                     string
		source                   *schema.Source
		webhookConfig            *webhookconfigcache.WebhookMatchingConfig
		excludedNamespaces       []string
		exemptedNamespaces       []string
		expectedMatchConditions  int
		expectedResourceRules    int
		hasNamespaceSelector     bool
		hasObjectSelector        bool
		hasWebhookMatchCondition bool
		hasExcludedCondition     bool
		hasExemptedCondition     bool
		expectedErr              error
	}{
		{
			name:                    "with nil webhook config",
			kind:                    "TestPolicy",
			source:                  baseSource,
			webhookConfig:           nil,
			excludedNamespaces:      nil,
			exemptedNamespaces:      nil,
			expectedMatchConditions: 5, // 1 template + 4 standard matchers
			expectedResourceRules:   1, // default wildcard rule
			hasNamespaceSelector:    false,
			hasObjectSelector:       false,
		},
		{
			name:   "with webhook config and rules",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
			},
			excludedNamespaces:      nil,
			exemptedNamespaces:      nil,
			expectedMatchConditions: 5,
			expectedResourceRules:   1,
			hasNamespaceSelector:    false,
			hasObjectSelector:       false,
		},
		{
			name:   "with webhook config including selectors",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
				ObjectSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
			},
			excludedNamespaces:      nil,
			exemptedNamespaces:      nil,
			expectedMatchConditions: 5,
			expectedResourceRules:   1,
			hasNamespaceSelector:    true,
			hasObjectSelector:       true,
		},
		{
			name:   "with webhook config and match conditions",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"configmaps"},
						},
					},
				},
				MatchConditions: []admissionregistrationv1.MatchCondition{
					{
						Name:       "webhook_user_check",
						Expression: "request.userInfo.username != 'system:admin'",
					},
				},
			},
			excludedNamespaces:       nil,
			exemptedNamespaces:       nil,
			expectedMatchConditions:  6, // 1 template + 4 standard + 1 webhook
			expectedResourceRules:    1,
			hasWebhookMatchCondition: true,
		},
		{
			name:                    "with excluded namespaces",
			kind:                    "TestPolicy",
			source:                  baseSource,
			webhookConfig:           nil,
			excludedNamespaces:      []string{"kube-system", "kube-public"},
			exemptedNamespaces:      nil,
			expectedMatchConditions: 6, // 1 template + 4 standard + 1 excluded
			expectedResourceRules:   1,
			hasExcludedCondition:    true,
		},
		{
			name:                    "with exempted namespaces",
			kind:                    "TestPolicy",
			source:                  baseSource,
			webhookConfig:           nil,
			excludedNamespaces:      nil,
			exemptedNamespaces:      []string{"gatekeeper-system"},
			expectedMatchConditions: 6, // 1 template + 4 standard + 1 exempted
			expectedResourceRules:   1,
			hasExemptedCondition:    true,
		},
		{
			name:                    "with both excluded and exempted namespaces",
			kind:                    "TestPolicy",
			source:                  baseSource,
			webhookConfig:           nil,
			excludedNamespaces:      []string{"kube-system"},
			exemptedNamespaces:      []string{"gatekeeper-system"},
			expectedMatchConditions: 7, // 1 template + 4 standard + 1 excluded + 1 exempted
			expectedResourceRules:   1,
			hasExcludedCondition:    true,
			hasExemptedCondition:    true,
		},
		{
			name:   "with webhook config and namespace exclusions",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"monitor": "true"},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
							admissionregistrationv1.Delete,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Resources:   []string{"*"},
						},
					},
				},
				MatchConditions: []admissionregistrationv1.MatchCondition{
					{
						Name:       "check_namespace",
						Expression: "object.metadata.namespace != 'default'",
					},
				},
			},
			excludedNamespaces:       []string{"kube-system", "kube-public"},
			exemptedNamespaces:       []string{"gatekeeper-system", "monitoring"},
			expectedMatchConditions:  8, // 1 template + 4 standard + 1 webhook + 1 excluded + 1 exempted
			expectedResourceRules:    1,
			hasNamespaceSelector:     true,
			hasWebhookMatchCondition: true,
			hasExcludedCondition:     true,
			hasExemptedCondition:     true,
		},
		{
			name:   "with multiple webhook rules",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"services"},
						},
					},
				},
			},
			excludedNamespaces:      nil,
			exemptedNamespaces:      nil,
			expectedMatchConditions: 5,
			expectedResourceRules:   2,
		},
		{
			name:   "with webhook config and match policy",
			kind:   "TestPolicy",
			source: baseSource,
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				MatchPolicy: (*admissionregistrationv1.MatchPolicyType)(ptr.To(string(admissionregistrationv1.Equivalent))),
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
			},
			excludedNamespaces:      nil,
			exemptedNamespaces:      nil,
			expectedMatchConditions: 5,
			expectedResourceRules:   1,
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

			policy, err := TemplateToPolicyDefinitionWithWebhookConfig(
				template,
				test.webhookConfig,
				test.excludedNamespaces,
				test.exemptedNamespaces,
			)

			if !errors.Is(err, test.expectedErr) {
				t.Errorf("unexpected error. got %v; wanted %v", err, test.expectedErr)
				return
			}

			if test.expectedErr != nil {
				return // Expected an error, no need to check further
			}

			if policy == nil {
				t.Fatal("expected non-nil policy")
				return
			}

			// Verify match conditions count
			if len(policy.Spec.MatchConditions) != test.expectedMatchConditions {
				t.Errorf("expected %d match conditions, got %d", test.expectedMatchConditions, len(policy.Spec.MatchConditions))
			}

			// Verify resource rules count
			if len(policy.Spec.MatchConstraints.ResourceRules) != test.expectedResourceRules {
				t.Errorf("expected %d resource rules, got %d", test.expectedResourceRules, len(policy.Spec.MatchConstraints.ResourceRules))
			}

			// Verify namespace selector
			if test.hasNamespaceSelector {
				if policy.Spec.MatchConstraints.NamespaceSelector == nil {
					t.Error("expected namespace selector but got nil")
				}
			}

			// Verify object selector
			if test.hasObjectSelector {
				if policy.Spec.MatchConstraints.ObjectSelector == nil {
					t.Error("expected object selector but got nil")
				}
			}

			// Verify webhook match condition exists
			if test.hasWebhookMatchCondition {
				found := false
				for _, cond := range policy.Spec.MatchConditions {
					if strings.Contains(cond.Name, "webhook") || strings.Contains(cond.Name, "check") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected webhook match condition but not found")
				}
			}

			// Verify excluded namespaces condition
			if test.hasExcludedCondition {
				found := false
				for _, cond := range policy.Spec.MatchConditions {
					if strings.Contains(cond.Name, "global_excluded") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected excluded namespaces condition but not found")
				}
			}

			// Verify exempted namespaces condition
			if test.hasExemptedCondition {
				found := false
				for _, cond := range policy.Spec.MatchConditions {
					if strings.Contains(cond.Name, "global_exempted") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected exempted namespaces condition but not found")
				}
			}

			// Verify policy name format
			expectedName := fmt.Sprintf("gatekeeper-%s", strings.ToLower(test.kind))
			if policy.Name != expectedName {
				t.Errorf("expected policy name %q, got %q", expectedName, policy.Name)
			}

			// Verify ParamKind is set correctly
			if policy.Spec.ParamKind == nil {
				t.Fatal("expected non-nil ParamKind")
			}
			if policy.Spec.ParamKind.Kind != test.kind {
				t.Errorf("expected ParamKind.Kind %q, got %q", test.kind, policy.Spec.ParamKind.Kind)
			}

			// Verify standard variables are present
			foundObject := false
			foundParams := false
			for _, v := range policy.Spec.Variables {
				if v.Name == schema.ObjectName {
					foundObject = true
				}
				if v.Name == schema.ParamsName {
					foundParams = true
				}
			}
			if !foundObject {
				t.Error("expected object variable but not found")
			}
			if !foundParams {
				t.Error("expected params variable but not found")
			}
		})
	}
}

func TestConvertWebhookRulesToResourceRules(t *testing.T) {
	tests := []struct {
		name             string
		rules            []admissionregistrationv1beta1.RuleWithOperations
		ctOps            []admissionregistrationv1beta1.OperationType
		expectedCount    int
		expectError      bool
		validateFirst    bool
		expectedError    error
		expectedOpsCount int
	}{
		{
			name:             "empty rules",
			rules:            []admissionregistrationv1beta1.RuleWithOperations{},
			ctOps:            nil,
			expectedCount:    0,
			expectError:      false,
			validateFirst:    false,
			expectedOpsCount: 0,
		},
		{
			name: "single rule with nil ctOps",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            nil,
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedOpsCount: 1,
		},
		{
			name: "single rule with matching ctOps",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedOpsCount: 1,
		},
		{
			name: "single rule with no operation match",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Update},
			expectedCount:    0,
			expectError:      true,
			validateFirst:    true,
			expectedError:    ErrOperationNoMatch,
			expectedOpsCount: 0,
		},
		{
			name: "multiple rules with ctOps",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Delete, admissionregistrationv1beta1.Create},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
			expectedCount:    2,
			expectError:      false,
			validateFirst:    true,
			expectedError:    ErrOperationMismatch,
			expectedOpsCount: 1,
		},
		{
			name: "webhook rule with wildcard operation and specific ctOps",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedOpsCount: 2,
		},
		{
			name: "specific webhook rule with wildcard ctOps",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedError:    ErrOperationMismatch,
			expectedOpsCount: 2,
		},
		{
			name: "both webhook and ctOps with wildcard",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedOpsCount: 4,
		},
		{
			name: "wildcard webhook with no matching specific ctOps should work",
			rules: []admissionregistrationv1beta1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
			ctOps:            []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Connect},
			expectedCount:    1,
			expectError:      false,
			validateFirst:    true,
			expectedOpsCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := convertWebhookRulesToResourceRules(test.rules, test.ctOps)

			// Check error expectation
			if test.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !test.expectError && err != nil {
				// Check if it's just a mismatch warning (which still returns results)
				if !errors.Is(err, ErrOperationMismatch) {
					t.Errorf("unexpected error: %v", err)
				}
			}

			// Check for specific expected error
			if test.expectedError != nil && err != nil {
				if !errors.Is(err, test.expectedError) {
					t.Errorf("expected %v but got: %v", test.expectedError, err)
				}
			}

			if len(result) != test.expectedCount {
				t.Errorf("expected %d resource rules, got %d", test.expectedCount, len(result))
			}

			if test.validateFirst && len(result) > 0 {
				if test.ctOps == nil {
					if len(result[0].Operations) != len(test.rules[0].Operations) {
						t.Errorf("expected %d operations, got %d", len(test.rules[0].Operations), len(result[0].Operations))
					}
				} else if !test.expectError {
					if len(result[0].Operations) != test.expectedOpsCount {
						t.Errorf("expected %d operations, got %d", test.expectedOpsCount, len(result[0].Operations))
					}
				}

				// Verify APIGroups, APIVersions, Resources are preserved
				if !reflect.DeepEqual(result[0].APIGroups, test.rules[0].APIGroups) {
					t.Errorf("APIGroups mismatch: got %v, want %v", result[0].APIGroups, test.rules[0].APIGroups)
				}
				if !reflect.DeepEqual(result[0].APIVersions, test.rules[0].APIVersions) {
					t.Errorf("APIVersions mismatch: got %v, want %v", result[0].APIVersions, test.rules[0].APIVersions)
				}
				if !reflect.DeepEqual(result[0].Resources, test.rules[0].Resources) {
					t.Errorf("Resources mismatch: got %v, want %v", result[0].Resources, test.rules[0].Resources)
				}
			}
		})
	}
}

func TestExpandWildcardOperations(t *testing.T) {
	allOps := []admissionregistrationv1beta1.OperationType{
		admissionregistrationv1beta1.Create,
		admissionregistrationv1beta1.Update,
		admissionregistrationv1beta1.Delete,
		admissionregistrationv1beta1.Connect,
	}

	tests := []struct {
		name     string
		ops      []admissionregistrationv1beta1.OperationType
		expected []admissionregistrationv1beta1.OperationType
	}{
		{
			name:     "wildcard operation expands to all",
			ops:      []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.OperationAll},
			expected: allOps,
		},
		{
			name:     "specific operations remain unchanged",
			ops:      []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
			expected: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
		},
		{
			name:     "empty operations remain empty",
			ops:      []admissionregistrationv1beta1.OperationType{},
			expected: []admissionregistrationv1beta1.OperationType{},
		},
		{
			name:     "wildcard with other operations expands to all",
			ops:      []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.OperationAll},
			expected: allOps,
		},
		{
			name:     "nil operations remain nil",
			ops:      nil,
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := expandWildcardOperations(test.ops, allOps)
			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestBuildMatchConstraintsFromWebhookConfig(t *testing.T) {
	tests := []struct {
		name                 string
		webhookConfig        *webhookconfigcache.WebhookMatchingConfig
		expectedRules        int
		hasNamespaceSelector bool
		hasObjectSelector    bool
		hasMatchPolicy       bool
	}{
		{
			name: "basic webhook config",
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
			},
			expectedRules:        1,
			hasNamespaceSelector: false,
			hasObjectSelector:    false,
			hasMatchPolicy:       false,
		},
		{
			name: "webhook config with selectors",
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				},
				ObjectSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
			},
			expectedRules:        1,
			hasNamespaceSelector: true,
			hasObjectSelector:    true,
			hasMatchPolicy:       false,
		},
		{
			name: "webhook config with match policy",
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				MatchPolicy: (*admissionregistrationv1.MatchPolicyType)(ptr.To(string(admissionregistrationv1.Exact))),
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"configmaps"},
						},
					},
				},
			},
			expectedRules:        1,
			hasNamespaceSelector: false,
			hasObjectSelector:    false,
			hasMatchPolicy:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Convert v1 rules to v1beta1 rules
			v1beta1Rules := make([]admissionregistrationv1beta1.RuleWithOperations, len(test.webhookConfig.Rules))
			for i, rule := range test.webhookConfig.Rules {
				v1beta1Ops := make([]admissionregistrationv1beta1.OperationType, len(rule.Operations))
				copy(v1beta1Ops, rule.Operations)
				v1beta1Rules[i] = admissionregistrationv1beta1.RuleWithOperations{
					Operations: v1beta1Ops,
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   rule.APIGroups,
						APIVersions: rule.APIVersions,
						Resources:   rule.Resources,
						Scope:       rule.Scope,
					},
				}
			}

			// Convert webhook rules to resource rules first
			resourceRules, err := convertWebhookRulesToResourceRules(v1beta1Rules, nil)
			if err != nil {
				t.Errorf("unexpected error converting rules: %v", err)
			}
			result := buildMatchConstraintsFromWebhookConfig(test.webhookConfig, resourceRules)

			if result == nil {
				t.Fatal("expected non-nil match constraints")
				return
			}

			if len(result.ResourceRules) != test.expectedRules {
				t.Errorf("expected %d resource rules, got %d", test.expectedRules, len(result.ResourceRules))
			}

			if test.hasNamespaceSelector && result.NamespaceSelector == nil {
				t.Error("expected namespace selector but got nil")
			}

			if test.hasObjectSelector && result.ObjectSelector == nil {
				t.Error("expected object selector but got nil")
			}

			if test.hasMatchPolicy && result.MatchPolicy == nil {
				t.Error("expected match policy but got nil")
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

func TestQuoteNamespaces(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []string
		expected   []string
	}{
		{
			name:       "empty list",
			namespaces: []string{},
			expected:   []string{},
		},
		{
			name:       "single namespace",
			namespaces: []string{"kube-system"},
			expected:   []string{`"kube-system"`},
		},
		{
			name:       "multiple namespaces",
			namespaces: []string{"kube-system", "default", "gatekeeper-system"},
			expected:   []string{`"kube-system"`, `"default"`, `"gatekeeper-system"`},
		},
		{
			name:       "namespace with special characters",
			namespaces: []string{"my-ns-123", "test_namespace"},
			expected:   []string{`"my-ns-123"`, `"test_namespace"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := quoteNamespaces(test.namespaces)
			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("got %v, want %v", result, test.expected)
			}
		})
	}
}

func TestBuildMatchConditions(t *testing.T) {
	tests := []struct {
		name                string
		source              *schema.Source
		excludedNamespaces  []string
		exemptedNamespaces  []string
		expectedConditions  int
		expectedErr         error
		checkExcludedExists bool
		checkExemptedExists bool
	}{
		{
			name: "basic conditions with no exclusions",
			source: &schema.Source{
				MatchConditions: []schema.MatchCondition{
					{Name: "test_condition", Expression: "true"},
				},
			},
			excludedNamespaces:  nil,
			exemptedNamespaces:  nil,
			expectedConditions:  5, // 1 from source + 4 standard matchers
			checkExcludedExists: false,
			checkExemptedExists: false,
		},
		{
			name: "with excluded namespaces",
			source: &schema.Source{
				MatchConditions: []schema.MatchCondition{
					{Name: "test_condition", Expression: "true"},
				},
			},
			excludedNamespaces:  []string{"kube-system", "default"},
			exemptedNamespaces:  nil,
			expectedConditions:  6, // 1 from source + 4 standard + 1 excluded
			checkExcludedExists: true,
			checkExemptedExists: false,
		},
		{
			name: "with exempted namespaces",
			source: &schema.Source{
				MatchConditions: []schema.MatchCondition{},
			},
			excludedNamespaces:  nil,
			exemptedNamespaces:  []string{"gatekeeper-system"},
			expectedConditions:  5, // 0 from source + 4 standard + 1 exempted
			checkExcludedExists: false,
			checkExemptedExists: true,
		},
		{
			name: "with both excluded and exempted namespaces",
			source: &schema.Source{
				MatchConditions: []schema.MatchCondition{
					{Name: "test1", Expression: "true"},
					{Name: "test2", Expression: "false"},
				},
			},
			excludedNamespaces:  []string{"kube-system"},
			exemptedNamespaces:  []string{"gatekeeper-system"},
			expectedConditions:  8, // 2 from source + 4 standard + 1 excluded + 1 exempted
			checkExcludedExists: true,
			checkExemptedExists: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conditions, err := buildMatchConditions(test.source, test.excludedNamespaces, test.exemptedNamespaces)

			if !errors.Is(err, test.expectedErr) {
				t.Errorf("unexpected error. got %v; wanted %v", err, test.expectedErr)
			}

			if len(conditions) != test.expectedConditions {
				t.Errorf("got %d conditions, want %d", len(conditions), test.expectedConditions)
			}

			// Check for excluded namespaces condition
			foundExcluded := false
			for _, cond := range conditions {
				if strings.Contains(cond.Name, "excluded") {
					foundExcluded = true
					break
				}
			}
			if test.checkExcludedExists && !foundExcluded {
				t.Error("expected excluded namespaces condition but not found")
			}

			// Check for exempted namespaces condition
			foundExempted := false
			for _, cond := range conditions {
				if strings.Contains(cond.Name, "exempted") {
					foundExempted = true
					break
				}
			}
			if test.checkExemptedExists && !foundExempted {
				t.Error("expected exempted namespaces condition but not found")
			}
		})
	}
}

func TestBuildVariables(t *testing.T) {
	tests := []struct {
		name              string
		source            *schema.Source
		expectedMinCount  int
		expectedErr       error
		checkUserVariable bool
	}{
		{
			name: "no user variables",
			source: &schema.Source{
				Variables: []schema.Variable{},
			},
			expectedMinCount:  2, // object and params
			checkUserVariable: false,
		},
		{
			name: "with user variables",
			source: &schema.Source{
				Variables: []schema.Variable{
					{Name: "my_var", Expression: "true"},
					{Name: "another_var", Expression: "false"},
				},
			},
			expectedMinCount:  4, // 2 standard + 2 user
			checkUserVariable: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			variables, err := buildVariables(test.source)

			if !errors.Is(err, test.expectedErr) {
				t.Errorf("unexpected error. got %v; wanted %v", err, test.expectedErr)
			}

			if len(variables) < test.expectedMinCount {
				t.Errorf("got %d variables, want at least %d", len(variables), test.expectedMinCount)
			}

			// Verify standard variables exist
			foundObject := false
			foundParams := false
			for _, v := range variables {
				if v.Name == schema.ObjectName {
					foundObject = true
				}
				if v.Name == schema.ParamsName {
					foundParams = true
				}
			}

			if !foundObject {
				t.Error("expected object variable but not found")
			}
			if !foundParams {
				t.Error("expected params variable but not found")
			}

			// Check for user variables if expected
			if test.checkUserVariable {
				foundUserVar := false
				for _, v := range variables {
					if v.Name == "my_var" {
						foundUserVar = true
						break
					}
				}
				if !foundUserVar {
					t.Error("expected user variable 'my_var' but not found")
				}
			}
		})
	}
}

func TestBuildDefaultMatchConstraints(t *testing.T) {
	constraints := buildDefaultMatchConstraints()

	if constraints == nil {
		t.Fatal("expected non-nil match constraints")
		return
	}

	if len(constraints.ResourceRules) != 1 {
		t.Errorf("expected 1 resource rule, got %d", len(constraints.ResourceRules))
	}

	rule := constraints.ResourceRules[0]
	if len(rule.Operations) != 2 {
		t.Errorf("expected 2 operations, got %d", len(rule.Operations))
	}

	hasCreate := false
	hasUpdate := false
	for _, op := range rule.Operations {
		if op == admissionregistrationv1beta1.Create {
			hasCreate = true
		}
		if op == admissionregistrationv1beta1.Update {
			hasUpdate = true
		}
	}

	if !hasCreate {
		t.Error("expected Create operation")
	}
	if !hasUpdate {
		t.Error("expected Update operation")
	}

	// Check wildcard resources
	if len(rule.APIGroups) != 1 || rule.APIGroups[0] != "*" {
		t.Errorf("expected wildcard APIGroups, got %v", rule.APIGroups)
	}
	if len(rule.APIVersions) != 1 || rule.APIVersions[0] != "*" {
		t.Errorf("expected wildcard APIVersions, got %v", rule.APIVersions)
	}
	if len(rule.Resources) != 1 || rule.Resources[0] != "*" {
		t.Errorf("expected wildcard Resources, got %v", rule.Resources)
	}
}

func TestAppendWebhookMatchConditions(t *testing.T) {
	tests := []struct {
		name               string
		initialConditions  []admissionregistrationv1beta1.MatchCondition
		webhookConfig      *webhookconfigcache.WebhookMatchingConfig
		expectedCount      int
		checkWebhookExists bool
	}{
		{
			name: "nil webhook config",
			initialConditions: []admissionregistrationv1beta1.MatchCondition{
				{Name: "test1", Expression: "true"},
			},
			webhookConfig:      nil,
			expectedCount:      1,
			checkWebhookExists: false,
		},
		{
			name: "webhook config with no match conditions",
			initialConditions: []admissionregistrationv1beta1.MatchCondition{
				{Name: "test1", Expression: "true"},
			},
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				MatchConditions: []admissionregistrationv1.MatchCondition{},
			},
			expectedCount:      1,
			checkWebhookExists: false,
		},
		{
			name: "webhook config with match conditions",
			initialConditions: []admissionregistrationv1beta1.MatchCondition{
				{Name: "test1", Expression: "true"},
			},
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				MatchConditions: []admissionregistrationv1.MatchCondition{
					{Name: "webhook_condition", Expression: "request.userInfo.username != 'admin'"},
				},
			},
			expectedCount:      2,
			checkWebhookExists: true,
		},
		{
			name:              "empty initial conditions with webhook conditions",
			initialConditions: []admissionregistrationv1beta1.MatchCondition{},
			webhookConfig: &webhookconfigcache.WebhookMatchingConfig{
				MatchConditions: []admissionregistrationv1.MatchCondition{
					{Name: "webhook1", Expression: "true"},
					{Name: "webhook2", Expression: "false"},
				},
			},
			expectedCount:      2,
			checkWebhookExists: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := appendWebhookMatchConditions(test.initialConditions, test.webhookConfig)

			if len(result) != test.expectedCount {
				t.Errorf("expected %d conditions, got %d", test.expectedCount, len(result))
			}

			if test.checkWebhookExists {
				foundWebhook := false
				for _, cond := range result {
					if strings.Contains(cond.Name, "webhook") {
						foundWebhook = true
						break
					}
				}
				if !foundWebhook {
					t.Error("expected webhook condition but not found")
				}
			}
		})
	}
}
