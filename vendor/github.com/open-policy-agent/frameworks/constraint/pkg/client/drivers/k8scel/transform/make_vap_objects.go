package transform

import (
	"fmt"
	"strings"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TemplateToPolicyDefinition(template *templates.ConstraintTemplate) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	source, err := schema.GetSourceFromTemplate(template)
	if err != nil {
		return nil, err
	}

	matchConditions, err := source.GetV1Beta1MatchConditions()
	if err != nil {
		return nil, err
	}
	matchConditions = append(matchConditions, AllMatchersV1Beta1()...)

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

	policy := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("gatekeeper-%s", template.GetName()),
		},
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
			ParamKind: &admissionregistrationv1beta1.ParamKind{
				APIVersion: fmt.Sprintf("%s/%s", apiconstraints.Group, templatesv1beta1.SchemeGroupVersion.Version),
				Kind:       template.Spec.CRD.Spec.Names.Kind,
			},
			MatchConstraints: &admissionregistrationv1beta1.MatchResources{
				ResourceRules: []admissionregistrationv1beta1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
							/// TODO(ritazh): default for now until we can safely expose these to users
							Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
							Rule:       admissionregistrationv1beta1.Rule{APIGroups: []string{"*"}, APIVersions: []string{"*"}, Resources: []string{"*"}},
						},
					},
				},
			},
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
		case string(apiconstraints.Deny):
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Deny)
		case string(apiconstraints.Warn):
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Warn)
		default:
			return nil, fmt.Errorf("%w: unrecognized enforcement action %s, must be `warn` or `deny`", ErrBadEnforcementAction, action)
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
