package constraint

import (
	"context"
	"errors"
	"fmt"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	webhookConfig, excludedNamespaces, exemptedNamespaces := r.vapMatchingConfig()
	return transform.ConstraintToPolicyDefinitionWithWebhookConfig(template, constraint, webhookConfig, excludedNamespaces, exemptedNamespaces)
}

func (r *ReconcileConstraint) vapMatchingConfig() (*webhookconfigcache.WebhookMatchingConfig, []string, []string) {
	if !*transform.SyncVAPScope {
		return nil, nil, nil
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
	return webhookConfig, excludedNamespaces, exemptedNamespaces
}

func (r *ReconcileConstraint) sharedVAPIsCurrent(ctx context.Context, templateName string, groupVersion *schema.GroupVersion) (bool, error) {
	if r.apiReader == nil {
		return false, errors.New("API reader is not configured")
	}
	template := &templatesv1beta1.ConstraintTemplate{}
	if err := r.apiReader.Get(ctx, types.NamespacedName{Name: templateName}, template); err != nil {
		return false, err
	}
	current, err := vapForVersion(groupVersion)
	if err != nil {
		return false, err
	}
	if err := r.apiReader.Get(ctx, types.NamespacedName{Name: transform.GetTemplateVAPName(templateName)}, current); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !template.GetDeletionTimestamp().IsZero() || !current.GetDeletionTimestamp().IsZero() || !metav1.IsControlledBy(current, template) {
		return false, nil
	}
	unversioned := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(template, unversioned, nil); err != nil {
		return false, err
	}
	eligible, err := ShouldGenerateVAP(unversioned)
	if err != nil || !eligible {
		return false, err
	}
	webhookConfig, excludedNamespaces, exemptedNamespaces, err := r.authoritativeVAPMatchingConfig(ctx)
	if err != nil {
		return false, err
	}
	desired, transformErr := transform.TemplateToPolicyDefinitionWithWebhookConfig(unversioned, webhookConfig, excludedNamespaces, exemptedNamespaces)
	if transformErr != nil && (desired == nil || !errors.Is(transformErr, transform.ErrOperationMismatch)) {
		return false, transformErr
	}
	proposed, err := getRunTimeVAP(groupVersion, desired, current)
	if err != nil {
		return false, err
	}
	return vapEqual(current, proposed), nil
}

func (r *ReconcileConstraint) authoritativeVAPMatchingConfig(ctx context.Context) (*webhookconfigcache.WebhookMatchingConfig, []string, []string, error) {
	if !*transform.SyncVAPScope {
		return nil, nil, nil, nil
	}
	config := &configv1alpha1.Config{}
	excluder := process.New()
	if err := r.apiReader.Get(ctx, keys.Config, config); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, nil, nil, err
		}
	} else if config.GetDeletionTimestamp().IsZero() {
		excluder.Add(config.Spec.Match)
	}
	configuration := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	var matching *webhookconfigcache.WebhookMatchingConfig
	if err := r.apiReader.Get(ctx, types.NamespacedName{Name: *webhook.VwhName}, configuration); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, nil, nil, err
		}
	} else {
		for index := range configuration.Webhooks {
			candidate := &configuration.Webhooks[index]
			if candidate.Name == webhook.ValidatingWebhookName {
				matching = &webhookconfigcache.WebhookMatchingConfig{
					NamespaceSelector: candidate.NamespaceSelector,
					ObjectSelector:    candidate.ObjectSelector,
					Rules:             candidate.Rules,
					MatchPolicy:       candidate.MatchPolicy,
					MatchConditions:   candidate.MatchConditions,
				}
				break
			}
		}
	}
	return matching, excluder.GetExcludedNamespaces(process.Webhook), webhook.GetAllExemptedNamespacesWithWildcard(), nil
}

func vapEqual(current, proposed runtime.Object) bool {
	currentCopy := current.DeepCopyObject()
	proposedCopy := proposed.DeepCopyObject()
	normalizeVAPMatchDefaults(currentCopy)
	normalizeVAPMatchDefaults(proposedCopy)
	return apiequality.Semantic.DeepEqual(currentCopy, proposedCopy)
}

func normalizeVAPMatchDefaults(policy runtime.Object) {
	normalizeSelector := func(selector **metav1.LabelSelector) {
		if *selector != nil && len((*selector).MatchLabels) == 0 && len((*selector).MatchExpressions) == 0 {
			*selector = nil
		}
	}
	normalizeRule := func(rule *admissionregistrationv1.Rule) {
		if rule.Scope != nil && *rule.Scope == admissionregistrationv1.AllScopes {
			rule.Scope = nil
		}
	}
	switch typed := policy.(type) {
	case *admissionregistrationv1.ValidatingAdmissionPolicy:
		if match := typed.Spec.MatchConstraints; match != nil {
			normalizeSelector(&match.NamespaceSelector)
			normalizeSelector(&match.ObjectSelector)
			if match.MatchPolicy != nil && *match.MatchPolicy == admissionregistrationv1.Equivalent {
				match.MatchPolicy = nil
			}
			for index := range match.ResourceRules {
				normalizeRule(&match.ResourceRules[index].Rule)
			}
			for index := range match.ExcludeResourceRules {
				normalizeRule(&match.ExcludeResourceRules[index].Rule)
			}
		}
	case *admissionregistrationv1beta1.ValidatingAdmissionPolicy:
		if match := typed.Spec.MatchConstraints; match != nil {
			normalizeSelector(&match.NamespaceSelector)
			normalizeSelector(&match.ObjectSelector)
			if match.MatchPolicy != nil && *match.MatchPolicy == admissionregistrationv1beta1.Equivalent {
				match.MatchPolicy = nil
			}
			for index := range match.ResourceRules {
				normalizeRule(&match.ResourceRules[index].Rule)
			}
			for index := range match.ExcludeResourceRules {
				normalizeRule(&match.ExcludeResourceRules[index].Rule)
			}
		}
	}
}

func bindingReferencesPolicy(binding client.Object, policyName string) bool {
	switch typed := binding.(type) {
	case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
		return typed.Spec.PolicyName == policyName
	case *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding:
		return typed.Spec.PolicyName == policyName
	default:
		return false
	}
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
	if current != nil && !metav1.IsControlledBy(current, constraint) {
		return "", fmt.Errorf("validatingadmissionpolicy %q exists but is not controlled by constraint %q", name, constraint.GetName())
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
	} else if !vapEqual(current, proposed) {
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
	owned, err := r.canDeleteConstraintResource(ctx, current, constraint)
	if err != nil {
		return err
	}
	if !owned {
		log.Info("VAP exists but is not owned by this constraint, skipping delete", "vapName", name, "constraintName", constraint.GetName(), "constraintKind", constraint.GetKind())
		return nil
	}
	if err := r.writer.Delete(ctx, current, client.Preconditions{UID: ptr.To(current.GetUID()), ResourceVersion: ptr.To(current.GetResourceVersion())}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
