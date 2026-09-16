package constraint

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func vapForVersion(gvk *schema.GroupVersion) (client.Object, error) {
	switch gvk.Version {
	case vapAPIVersionV1:
		return &admissionregistrationv1.ValidatingAdmissionPolicy{}, nil
	case vapAPIVersionV1Beta1:
		return &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}, nil
	default:
		return nil, errors.New("unrecognized version")
	}
}

func getRunTimeVAP(gvk *schema.GroupVersion, transformed *admissionregistrationv1beta1.ValidatingAdmissionPolicy, current client.Object) (client.Object, error) {
	if current == nil {
		if gvk.Version == vapAPIVersionV1 {
			return v1beta1VAPToV1(transformed)
		}
		return transformed.DeepCopy(), nil
	}
	if gvk.Version == vapAPIVersionV1 {
		currentVAP, ok := current.(*admissionregistrationv1.ValidatingAdmissionPolicy)
		if !ok {
			return nil, errors.New("unable to convert current VAP to v1")
		}
		proposed, err := v1beta1VAPToV1(transformed)
		if err != nil {
			return nil, err
		}
		result := currentVAP.DeepCopy()
		result.Spec = proposed.Spec
		return result, nil
	}
	currentVAP, ok := current.(*admissionregistrationv1beta1.ValidatingAdmissionPolicy)
	if !ok {
		return nil, errors.New("unable to convert current VAP to v1beta1")
	}
	result := currentVAP.DeepCopy()
	result.Spec = transformed.Spec
	return result, nil
}

func v1beta1VAPToV1(source *admissionregistrationv1beta1.ValidatingAdmissionPolicy) (*admissionregistrationv1.ValidatingAdmissionPolicy, error) {
	result := &admissionregistrationv1.ValidatingAdmissionPolicy{ObjectMeta: *source.ObjectMeta.DeepCopy()}
	if source.Spec.ParamKind != nil {
		result.Spec.ParamKind = &admissionregistrationv1.ParamKind{
			APIVersion: source.Spec.ParamKind.APIVersion,
			Kind:       source.Spec.ParamKind.Kind,
		}
	}
	if source.Spec.MatchConstraints != nil {
		matchConstraints := &admissionregistrationv1.MatchResources{
			NamespaceSelector: source.Spec.MatchConstraints.NamespaceSelector.DeepCopy(),
			ObjectSelector:    source.Spec.MatchConstraints.ObjectSelector.DeepCopy(),
		}
		if source.Spec.MatchConstraints.MatchPolicy != nil {
			matchPolicy := admissionregistrationv1.MatchPolicyType(*source.Spec.MatchConstraints.MatchPolicy)
			matchConstraints.MatchPolicy = &matchPolicy
		}
		for index := range source.Spec.MatchConstraints.ResourceRules {
			rule := source.Spec.MatchConstraints.ResourceRules[index]
			matchConstraints.ResourceRules = append(matchConstraints.ResourceRules, admissionregistrationv1.NamedRuleWithOperations{
				RuleWithOperations: admissionregistrationv1.RuleWithOperations{
					Operations: append([]admissionregistrationv1.OperationType(nil), rule.Operations...),
					Rule: admissionregistrationv1.Rule{
						APIGroups:   append([]string(nil), rule.APIGroups...),
						APIVersions: append([]string(nil), rule.APIVersions...),
						Resources:   append([]string(nil), rule.Resources...),
						Scope:       rule.Scope,
					},
				},
			})
		}
		result.Spec.MatchConstraints = matchConstraints
	}
	for _, condition := range source.Spec.MatchConditions {
		result.Spec.MatchConditions = append(result.Spec.MatchConditions, admissionregistrationv1.MatchCondition{
			Name:       condition.Name,
			Expression: condition.Expression,
		})
	}
	for _, validation := range source.Spec.Validations {
		result.Spec.Validations = append(result.Spec.Validations, admissionregistrationv1.Validation{
			Expression:        validation.Expression,
			Message:           validation.Message,
			Reason:            validation.Reason,
			MessageExpression: validation.MessageExpression,
		})
	}
	if source.Spec.FailurePolicy != nil {
		failurePolicy := admissionregistrationv1.FailurePolicyType(*source.Spec.FailurePolicy)
		switch failurePolicy {
		case admissionregistrationv1.Fail, admissionregistrationv1.Ignore:
			result.Spec.FailurePolicy = &failurePolicy
		default:
			return nil, fmt.Errorf("%w: unrecognized failure policy: %s", celSchema.ErrBadFailurePolicy, failurePolicy)
		}
	}
	for _, variable := range source.Spec.Variables {
		result.Spec.Variables = append(result.Spec.Variables, admissionregistrationv1.Variable{
			Name:       variable.Name,
			Expression: variable.Expression,
		})
	}
	return result, nil
}

func (r *ReconcileConstraint) transformConstraintToVAP(template *templates.ConstraintTemplate, constraint *unstructured.Unstructured) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	if !*transform.SyncVAPScope {
		return transform.ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, nil, nil, nil)
	}
	var excludedNamespaces []string
	if r.processExcluder != nil {
		excludedNamespaces = r.processExcluder.GetExcludedNamespaces(process.Webhook)
	}
	exemptedNamespaces := webhook.GetAllExemptedNamespacesWithWildcard()
	var webhookConfig *webhookconfigcache.WebhookMatchingConfig
	if r.webhookConfigCache != nil {
		if config, found := r.webhookConfigCache.GetConfig(*webhook.VwhName); found {
			webhookConfig = &config
		}
	}
	return transform.ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, webhookConfig, excludedNamespaces, exemptedNamespaces)
}

func (r *ReconcileConstraint) reconcileConstraintVAP(ctx context.Context, template *templates.ConstraintTemplate, constraint *unstructured.Unstructured, groupVersion *schema.GroupVersion) (string, error) {
	name := transform.GetConstraintVAPName(constraint.GetKind(), constraint.GetName())
	current, err := vapForVersion(groupVersion)
	if err != nil {
		return "", err
	}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: name}, current); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		current = nil
	}
	transformed, transformErr := r.transformConstraintToVAP(template, constraint)
	if transformErr != nil && (transformed == nil || !errors.Is(transformErr, transform.ErrOperationMismatch)) {
		return "", transformErr
	}
	proposed, err := getRunTimeVAP(groupVersion, transformed, current)
	if err != nil {
		return "", err
	}
	if err := controllerutil.SetControllerReference(constraint, proposed, r.scheme); err != nil {
		return "", err
	}
	if current == nil {
		err = r.writer.Create(ctx, proposed)
	} else if !reflect.DeepEqual(current, proposed) {
		err = r.writer.Update(ctx, proposed)
	}
	if err != nil {
		return "", err
	}
	warning := ""
	if transformErr != nil {
		warning = transformErr.Error()
		logf.FromContext(ctx).Info("generated per-Constraint VAP with operation mismatch", "vapName", name, "warning", warning)
	}
	return warning, nil
}

func (r *ReconcileConstraint) deleteConstraintVAPIfOwned(ctx context.Context, constraint *unstructured.Unstructured, groupVersion *schema.GroupVersion) error {
	if groupVersion == nil {
		return nil
	}
	name := transform.GetConstraintVAPName(constraint.GetKind(), constraint.GetName())
	current, err := vapForVersion(groupVersion)
	if err != nil {
		return err
	}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: name}, current); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !vapControlledByConstraint(current, constraint) {
		log.Info("VAP exists but is not owned by this constraint, skipping delete", "vapName", name, "constraintName", constraint.GetName(), "constraintKind", constraint.GetKind())
		return nil
	}
	if err := r.writer.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func vapControlledByConstraint(policy metav1.Object, constraint *unstructured.Unstructured) bool {
	return vapBindingControlledByConstraint(policy, constraint)
}
