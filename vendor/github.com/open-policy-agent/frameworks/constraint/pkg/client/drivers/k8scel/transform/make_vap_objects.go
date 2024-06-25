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

const VAPEnforcementPoint = "vap.k8s.io"

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

func ConstraintToBinding(constraint *unstructured.Unstructured, enforcementActions []admissionregistrationv1beta1.ValidationAction) (*admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding, error) {
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

func AssumeVAPEnforcement(ct *templates.ConstraintTemplate, generateVAPDefault bool) (bool, error) {
	source, err := schema.GetSourceFromTemplate(ct)
	if err != nil {
		return false, err
	}
	if source.GenerateVAP == nil {
		return generateVAPDefault, nil
	}
	return *source.GenerateVAP, nil
}

func ShouldGenerateVAPB(constraint *unstructured.Unstructured) (bool, []admissionregistrationv1beta1.ValidationAction ,error) {
	enforcementActionStr, err := apiconstraints.GetEnforcementAction(constraint)
	var enforcementActions []admissionregistrationv1beta1.ValidationAction
	if err != nil {
		return false, enforcementActions, err
	}

	actions := []string{}
	if apiconstraints.IsEnforcementActionScoped(enforcementActionStr) {
		actionsForEP, err := apiconstraints.GetEnforcementActionsForEP(constraint, []string{VAPEnforcementPoint})
		if err != nil {
			return false, enforcementActions, err
		}
		if len(actionsForEP[VAPEnforcementPoint]) == 0 {
			return false, enforcementActions, nil
		}
		for action := range actionsForEP[VAPEnforcementPoint] {
			actions = append(actions, action)
		}
	}


	for _, action := range actions {
		switch action {
		case apiconstraints.EnforcementActionDeny:
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Deny)
		case "warn":
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Warn)
		default:
			return false, enforcementActions, fmt.Errorf("%w: unrecognized enforcement action %s, must be `warn` or `deny`", ErrBadEnforcementAction, action)
		}
	}

	// TO-DO, in beta when we turn on VAP, VAPB generation by default, this will throw an error for contraints that have enforcementAction: dryrun
	// as dryrun is not a valid enforcement action for VAPB. We need to handle this case.
	// Option 1: Throw an error for constraints on status that have enforcementAction: dryrun, and still create the constraint. Skip on creating VAPB.
	// Option 2: Define a default enforcement action for VAPB, and use that for constraints that have enforcementAction: `unrecognized`.
	if len(enforcementActions) == 0 {
		switch enforcementActionStr {
		case apiconstraints.EnforcementActionDeny:
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Deny)
		case "warn":
			enforcementActions = append(enforcementActions, admissionregistrationv1beta1.Warn)
		default:
			return false, enforcementActions, fmt.Errorf("%w: unrecognized enforcement action %s, must be `warn` or `deny`", ErrBadEnforcementAction, enforcementActionStr)
		}
	}
	return true, enforcementActions, nil
}
