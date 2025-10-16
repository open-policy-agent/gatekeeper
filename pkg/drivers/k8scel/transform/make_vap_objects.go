package transform

import (
	"flag"
	"fmt"
	"strings"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

var SyncVAPScope = flag.Bool("sync-vap-enforcement-scope", false, "(alpha) Synchronize ValidatingAdmissionPolicy enforcement scope with Gatekeeper's admission validation scope. When enabled, VAP resources inherit match criteria, conditions, and namespace exclusions from Gatekeeper's webhook configuration, Config resource and exempt namespace flags. This ensures consistent policy enforcement between Gatekeeper and VAP but triggers constraint template reconciliation on scope changes in Config resource or webhook configuration. This flag will be removed in future release.")

func TemplateToPolicyDefinition(template *templates.ConstraintTemplate) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	return TemplateToPolicyDefinitionWithWebhookConfig(template, nil, nil, nil)
}

// quoteNamespaces wraps each namespace string in quotes for proper CEL syntax.
func quoteNamespaces(namespaces []string) []string {
	quoted := make([]string, len(namespaces))
	for i, ns := range namespaces {
		quoted[i] = fmt.Sprintf(`"%s"`, ns)
	}
	return quoted
}

// buildMatchConditions constructs the complete list of match conditions for the VAP policy.
func buildMatchConditions(source *schema.Source, excludedNamespaces, exemptedNamespaces []string) ([]admissionregistrationv1beta1.MatchCondition, error) {
	// Start with template-defined match conditions
	matchConditions, err := source.GetV1Beta1MatchConditions()
	if err != nil {
		return nil, err
	}

	// Add standard matchers
	matchConditions = append(matchConditions, AllMatchersV1Beta1()...)

	// Add excluded namespaces condition if specified
	if len(excludedNamespaces) > 0 {
		quotedNamespaces := quoteNamespaces(excludedNamespaces)
		matchConditions = append(matchConditions,
			MatchGlobalExcludedNamespacesGlobV1Beta1(strings.Join(quotedNamespaces, ",")))
	}

	// Add exempted namespaces condition if specified
	if len(exemptedNamespaces) > 0 {
		quotedNamespaces := quoteNamespaces(exemptedNamespaces)
		matchConditions = append(matchConditions,
			MatchGlobalExemptedNamespacesGlobV1Beta1(strings.Join(quotedNamespaces, ",")))
	}

	return matchConditions, nil
}

// buildVariables constructs the complete list of variables for the VAP policy.
func buildVariables(source *schema.Source) ([]admissionregistrationv1beta1.Variable, error) {
	variables := AllVariablesV1Beta1()
	userVariables, err := source.GetV1Beta1Variables()
	if err != nil {
		return nil, err
	}

	return append(variables, userVariables...), nil
}

// convertWebhookRulesToResourceRules converts webhook rules to VAP resource rules.
func convertWebhookRulesToResourceRules(rules []admissionregistrationv1beta1.RuleWithOperations) []admissionregistrationv1beta1.NamedRuleWithOperations {
	resourceRules := make([]admissionregistrationv1beta1.NamedRuleWithOperations, 0, len(rules))

	for _, rule := range rules {
		// Convert operations from webhook format to VAP format
		operations := make([]admissionregistrationv1beta1.OperationType, len(rule.Operations))
		copy(operations, rule.Operations)

		resourceRules = append(resourceRules, admissionregistrationv1beta1.NamedRuleWithOperations{
			RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
				Operations: operations,
				Rule: admissionregistrationv1beta1.Rule{
					APIGroups:   rule.APIGroups,
					APIVersions: rule.APIVersions,
					Resources:   rule.Resources,
					Scope:       rule.Scope,
				},
			},
		})
	}

	return resourceRules
}

// buildMatchConstraintsFromWebhookConfig creates MatchResources from webhook configuration.
func buildMatchConstraintsFromWebhookConfig(webhookConfig *webhookconfigcache.WebhookMatchingConfig) *admissionregistrationv1beta1.MatchResources {
	resourceRules := convertWebhookRulesToResourceRules(webhookConfig.Rules)

	return &admissionregistrationv1beta1.MatchResources{
		NamespaceSelector: webhookConfig.NamespaceSelector,
		ObjectSelector:    webhookConfig.ObjectSelector,
		ResourceRules:     resourceRules,
		MatchPolicy:       (*admissionregistrationv1beta1.MatchPolicyType)(webhookConfig.MatchPolicy),
	}
}

// buildDefaultMatchConstraints creates default MatchResources when no webhook config is provided.
func buildDefaultMatchConstraints() *admissionregistrationv1beta1.MatchResources {
	return &admissionregistrationv1beta1.MatchResources{
		ResourceRules: []admissionregistrationv1beta1.NamedRuleWithOperations{
			{
				RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
					Operations: []admissionregistrationv1beta1.OperationType{
						admissionregistrationv1beta1.Create,
						admissionregistrationv1beta1.Update,
					},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{"*"},
						APIVersions: []string{"*"},
						Resources:   []string{"*"},
					},
				},
			},
		},
	}
}

// appendWebhookMatchConditions adds webhook-specific match conditions to the policy.
func appendWebhookMatchConditions(matchConditions []admissionregistrationv1beta1.MatchCondition, webhookConfig *webhookconfigcache.WebhookMatchingConfig) []admissionregistrationv1beta1.MatchCondition {
	if webhookConfig == nil || len(webhookConfig.MatchConditions) == 0 {
		return matchConditions
	}

	for _, webhookCondition := range webhookConfig.MatchConditions {
		matchConditions = append(matchConditions, admissionregistrationv1beta1.MatchCondition{
			Name:       webhookCondition.Name,
			Expression: webhookCondition.Expression,
		})
	}

	return matchConditions
}

func TemplateToPolicyDefinitionWithWebhookConfig(template *templates.ConstraintTemplate, webhookConfig *webhookconfigcache.WebhookMatchingConfig, excludedNamespaces []string, exemptedNamespaces []string) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	// Extract CEL source from template
	source, err := schema.GetSourceFromTemplate(template)
	if err != nil {
		return nil, err
	}

	// Build match conditions (includes template conditions + namespace exclusions/exemptions)
	matchConditions, err := buildMatchConditions(source, excludedNamespaces, exemptedNamespaces)
	if err != nil {
		return nil, err
	}

	// Build validations from template
	validations, err := source.GetV1Beta1Validatons()
	if err != nil {
		return nil, err
	}

	// Build variables (includes standard + template-specific variables)
	variables, err := buildVariables(source)
	if err != nil {
		return nil, err
	}

	// Get failure policy from template
	failurePolicy, err := source.GetV1Beta1FailurePolicy()
	if err != nil {
		return nil, err
	}

	// Build match constraints based on webhook config availability
	var matchConstraints *admissionregistrationv1beta1.MatchResources
	if webhookConfig != nil {
		matchConstraints = buildMatchConstraintsFromWebhookConfig(webhookConfig)
		matchConditions = appendWebhookMatchConditions(matchConditions, webhookConfig)
	} else {
		matchConstraints = buildDefaultMatchConstraints()
	}

	// Construct the final ValidatingAdmissionPolicy
	policy := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("gatekeeper-%s", template.GetName()),
		},
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
			ParamKind: &admissionregistrationv1beta1.ParamKind{
				APIVersion: fmt.Sprintf("%s/%s", apiconstraints.Group, templatesv1beta1.SchemeGroupVersion.Version),
				Kind:       template.Spec.CRD.Spec.Names.Kind,
			},
			MatchConstraints: matchConstraints,
			MatchConditions:  matchConditions,
			Validations:      validations,
			FailurePolicy:    failurePolicy,
			AuditAnnotations: nil,
			Variables:        variables,
		},
	}

	return policy, nil
}

// ConstraintToBinding converts a Constraint to a ValidatingAdmissionPolicyBinding.
// Accepts a list of enforcement actions to apply to the binding.
// If the enforcement action is not recognized, returns an error.
func ConstraintToBinding(constraint *unstructured.Unstructured, actions []string) (*admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding, error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("%w: enforcement actions must be provided", ErrBadEnforcementAction)
	}
	var enforcementActions []admissionregistrationv1beta1.ValidationAction

	for _, action := range actions {
		switch action {
		case string(util.Deny):
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Deny)
		case string(util.Warn):
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Warn)
		case string(util.Dryrun):
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Audit)
		default:
			return nil, fmt.Errorf("%w: unrecognized enforcement action %s, must be `warn`, `deny` or `dryrun`", ErrBadEnforcementAction, action)
		}
	}

	binding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("gatekeeper-%s", constraint.GetName()),
		},
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: fmt.Sprintf("gatekeeper-%s", strings.ToLower(constraint.GetKind())),
			ParamRef: &admissionregistrationv1beta1.ParamRef{
				Name:                    constraint.GetName(),
				ParameterNotFoundAction: ptr.To[admissionregistrationv1beta1.ParameterNotFoundActionType](admissionregistrationv1beta1.AllowAction),
			},
			MatchResources:    &admissionregistrationv1beta1.MatchResources{},
			ValidationActions: enforcementActions,
		},
	}
	objectSelectorMap, found, err := unstructured.NestedMap(constraint.Object, "spec", "match", "labelSelector")
	if err != nil {
		return nil, err
	}
	var objectSelector *metav1.LabelSelector
	if found {
		objectSelector = &metav1.LabelSelector{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(objectSelectorMap, objectSelector); err != nil {
			return nil, err
		}
		binding.Spec.MatchResources.ObjectSelector = objectSelector
	}

	namespaceSelectorMap, found, err := unstructured.NestedMap(constraint.Object, "spec", "match", "namespaceSelector")
	if err != nil {
		return nil, err
	}
	var namespaceSelector *metav1.LabelSelector
	if found {
		namespaceSelector = &metav1.LabelSelector{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(namespaceSelectorMap, namespaceSelector); err != nil {
			return nil, err
		}
		binding.Spec.MatchResources.NamespaceSelector = namespaceSelector
	}
	return binding, nil
}
