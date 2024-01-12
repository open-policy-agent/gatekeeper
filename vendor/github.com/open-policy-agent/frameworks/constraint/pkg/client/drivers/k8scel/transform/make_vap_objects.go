package transform

import (
	"fmt"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionregistrationv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TemplateToPolicyDefinition(template *templates.ConstraintTemplate) (*admissionregistrationv1alpha1.ValidatingAdmissionPolicy, error) {
	source, err := schema.GetSourceFromTemplate(template)
	if err != nil {
		return nil, err
	}

	matchConditions, err := source.GetV1Alpha1MatchConditions()
	if err != nil {
		return nil, err
	}
	matchConditions = append(matchConditions, AllMatchersV1Alpha1()...)

	validations, err := source.GetV1Alpha1Validatons()
	if err != nil {
		return nil, err
	}

	variables, err := source.GetV1Alpha1Variables()
	if err != nil {
		return nil, err
	}
	variables = append(variables, AllVariablesV1Alpha1()...)

	failurePolicy, err := source.GetV1alpha1FailurePolicy()
	if err != nil {
		return nil, err
	}

	policy := &admissionregistrationv1alpha1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("g8r-%s", template.GetName()),
		},
		Spec: admissionregistrationv1alpha1.ValidatingAdmissionPolicySpec{
			ParamKind: &admissionregistrationv1alpha1.ParamKind{
				APIVersion: templatesv1beta1.SchemeGroupVersion.Version,
				Kind:       template.Spec.CRD.Spec.Names.Kind,
			},
			MatchConstraints: nil, // We cannot support match constraints since `resource` is not available shift-left
			MatchConditions:  matchConditions,
			Validations:      validations,
			FailurePolicy:    failurePolicy,
			AuditAnnotations: nil,
			Variables:        variables,
		},
	}
	return policy, nil
}
