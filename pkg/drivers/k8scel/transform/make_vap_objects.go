package transform

import (
	"fmt"
	"flag"
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

var SyncVAPScope = flag.Bool("sync-vap-enforcement-scope", false, "(alpha) Synchronize ValidatingAdmissionPolicy enforcement scope with Gatekeeper's admission validation scope. When enabled, VAP resources inherit match criteria, conditions, and namespace exclusions from Gatekeeper's webhook configuration, Config resource and exempt namespace flags. This ensures consistent policy enforcement between Gatekeeper and VAP but triggers constraint template reconciliation on scope changes in Config resource or webhook configuration.")

func TemplateToPolicyDefinition(template *templates.ConstraintTemplate) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	return TemplateToPolicyDefinitionWithWebhookConfig(template, nil, nil)
}

func TemplateToPolicyDefinitionWithWebhookConfig(template *templates.ConstraintTemplate, webhookConfig *webhookconfigcache.WebhookMatchingConfig, excludedNamespaces []string) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	source, err := schema.GetSourceFromTemplate(template)
	if err != nil {
		return nil, err
	}

	matchConditions, err := source.GetV1Beta1MatchConditions()
	if err != nil {
		return nil, err
	}
	matchConditions = append(matchConditions, AllMatchersV1Beta1()...)

	if len(excludedNamespaces) > 0 {
		// Quote each namespace for proper CEL syntax
		quotedNamespaces := make([]string, len(excludedNamespaces))
		for i, ns := range excludedNamespaces {
			quotedNamespaces[i] = fmt.Sprintf(`"%s"`, ns)
		}
		matchConditions = append(matchConditions, MatchGlobalExcludedNamespacesGlobV1Beta1(strings.Join(quotedNamespaces, ",")))
	}

	validations, err := source.GetV1Beta1Validatons()
	if err != nil {
		return nil, err
	}

	variables := AllVariablesV1Beta1()

	userVariables, err := source.GetV1Beta1Variables()
	if err != nil {
		return nil, err
	}
	variables = append(variables, userVariables...)

	failurePolicy, err := source.GetV1Beta1FailurePolicy()
	if err != nil {
		return nil, err
	}

	// Build match constraints from webhook config if available, otherwise use defaults
	var matchConstraints *admissionregistrationv1beta1.MatchResources
	if webhookConfig != nil {
		// Use webhook configuration for matching criteria
		resourceRules := make([]admissionregistrationv1beta1.NamedRuleWithOperations, 0, len(webhookConfig.Rules))
		for _, rule := range webhookConfig.Rules {
			// Convert operations from webhook format to VAP format
			operations := make([]admissionregistrationv1beta1.OperationType, 0, len(rule.Operations))
			operations = append(operations, rule.Operations...)

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

		matchConstraints = &admissionregistrationv1beta1.MatchResources{
			NamespaceSelector: webhookConfig.NamespaceSelector,
			ObjectSelector:    webhookConfig.ObjectSelector,
			ResourceRules:     resourceRules,
			MatchPolicy:       (*admissionregistrationv1beta1.MatchPolicyType)(webhookConfig.MatchPolicy),
		}

		// Add webhook match conditions to the policy match conditions
		if len(webhookConfig.MatchConditions) > 0 {
			for _, webhookCondition := range webhookConfig.MatchConditions {
				matchConditions = append(matchConditions, admissionregistrationv1beta1.MatchCondition{
					Name:       webhookCondition.Name,
					Expression: webhookCondition.Expression,
				})
			}
		}
	} else {
		// Default match constraints when no webhook config available
		matchConstraints = &admissionregistrationv1beta1.MatchResources{
			ResourceRules: []admissionregistrationv1beta1.NamedRuleWithOperations{
				{
					RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
						/// TODO(ritazh): default for now until we can safely expose these to users
						Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
						Rule:       admissionregistrationv1beta1.Rule{APIGroups: []string{"*"}, APIVersions: []string{"*"}, Resources: []string{"*"}},
					},
				},
			},
		}
	}

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
