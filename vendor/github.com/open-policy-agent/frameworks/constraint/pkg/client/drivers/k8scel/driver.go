package k8scel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/storage"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/validatingadmissionpolicy"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
	celAPI "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/authentication/user"
)

// NOTE: This is a PROTOTYPE driver. Do not use this for any critical work
// and be aware that its behavior may change at any time.

// Friction log:
//   there is no way to re-use the matcher interface here, as it requires an informer... not sure we need to use
//   the matchers, as match Criteria should take care of things.

//   "Expression" is a bit confusing, since it doesn't tell me whether "true" implies violation or not: "requirement", "mustSatisfy"?
//
//
//   From the Validation help text:
//      Equality on arrays with list type of 'set' or 'map' ignores element order, i.e. [1, 2] == [2, 1].
//      Concatenation on arrays with x-kubernetes-list-type use the semantics of the list type:
//   Is this type metadata available shift-left? Likely not. Can the expectation be built into the operators?
//
//   Other friction points are commented with the keyword FRICTION.

const (
	Name = "K8sNativeValidation"

	runTimeNS            = "runTimeNS"
	runTimeNSDescription = "the number of nanoseconds it took to evaluate the constraint"
)

var _ drivers.Driver = &Driver{}

type Driver struct {
	mux            sync.RWMutex
	validators     map[string]validatingadmissionpolicy.Validator
	filterCompiler cel.FilterCompiler
	gatherStats    bool
}

func (d *Driver) Name() string {
	return Name
}

func (d *Driver) AddTemplate(ctx context.Context, ct *templates.ConstraintTemplate) error {
	if len(ct.Spec.Targets) != 1 {
		return errors.New("wrong number of targets defined, only 1 target allowed")
	}
	var rawCode map[string]interface{}
	for _, code := range ct.Spec.Targets[0].Code {
		if code.Engine != Name {
			continue
		}
		objMap, ok := code.Source.Value.(map[string]interface{})
		if !ok {
			return errors.New("K8sNativeValidation code malformed")
		}
		rawCode = objMap
		break
	}
	if rawCode == nil {
		return errors.New("K8sNativeValidation code not defined")
	}

	validatorCode := &admissionv1alpha1.ValidatingAdmissionPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(rawCode, validatorCode, true); err != nil {
		return err
	}

	// FRICTION: Note that compilation errors are possible, but we cannot introspect to see whether any
	// occurred
	// TODO can set this based on whether params is defined on a constraint
	celVars := cel.OptionalVariableDeclarations{HasParams: true}
	failurePolicy := convertv1alpha1FailurePolicyTypeTov1FailurePolicyType(validatorCode.Spec.FailurePolicy)
	var matcher matchconditions.Matcher
	matchConditions := validatorCode.Spec.MatchConditions
	if len(matchConditions) > 0 {
		matchExpressionAccessors := make([]cel.ExpressionAccessor, len(matchConditions))
		for i := range matchConditions {
			matchExpressionAccessors[i] = (*matchconditions.MatchCondition)(&matchConditions[i])
		}
		matcher = matchconditions.NewMatcher(d.filterCompiler.Compile(matchExpressionAccessors, celVars, celAPI.PerCallLimit), nil, failurePolicy, "validatingadmissionpolicy", validatorCode.Name)
	}
	validator := validatingadmissionpolicy.NewValidator(
		d.filterCompiler.Compile(convertv1alpha1Validations(validatorCode.Spec.Validations), celVars, celAPI.PerCallLimit),
		matcher,
		d.filterCompiler.Compile(convertv1alpha1AuditAnnotations(validatorCode.Spec.AuditAnnotations), celVars, celAPI.PerCallLimit),
		d.filterCompiler.Compile(convertV1Alpha1MessageExpressions(validatorCode.Spec.Validations), celVars, celAPI.PerCallLimit),
		failurePolicy,
		nil,
	)

	d.mux.Lock()
	defer d.mux.Unlock()
	d.validators[ct.GetName()] = validator
	return nil
}

func (d *Driver) RemoveTemplate(ctx context.Context, ct *templates.ConstraintTemplate) error {
	d.mux.Lock()
	defer d.mux.Unlock()
	delete(d.validators, ct.GetName())
	return nil
}

func (d *Driver) AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	return nil
}

func (d *Driver) RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	return nil
}

func (d *Driver) AddData(ctx context.Context, target string, path storage.Path, data interface{}) error {
	return nil
}

func (d *Driver) RemoveData(ctx context.Context, target string, path storage.Path) error {
	return nil
}

func (d *Driver) Query(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...drivers.QueryOpt) (*drivers.QueryResponse, error) {
	cfg := &drivers.QueryCfg{}
	for _, opt := range opts {
		opt(cfg)
	}

	d.mux.RLock()
	defer d.mux.RUnlock()

	var statsEntries []*instrumentation.StatsEntry

	arGetter, ok := review.(ARGetter)
	if !ok {
		return nil, errors.New("cannot convert review to ARGetter")
	}
	aRequest := arGetter.GetAdmissionRequest()
	request, err := NewWrapper(aRequest)
	if err != nil {
		return nil, err
	}

	results := []*types.Result{}

	for _, constraint := range constraints {
		evalStartTime := time.Now()
		// FRICTION/design question: should parameters be created as a "mock" object so that users don't have to type `params.spec.parameters`? How do we prevent visibility into other,
		// non-parameter fields, such as `spec.match`? Does it matter? Note that creating a special "parameters" object means that we'd need to copy the constraint contents to
		// a special "parameters" object for on-server enforcement with a clean value for "params", which is non-ideal. Could we provide the field of the parameters object and limit scoping to that?
		// Then how would we implement custom matchers? Maybe adding variable assignments to the Policy Definition is a better idea? That would at least allow for a convenience handle, even if
		// it doesn't scope visibility.

		// template name is the lowercase of its kind
		validator := d.validators[strings.ToLower(constraint.GetKind())]
		if validator == nil {
			return nil, fmt.Errorf("unknown constraint template validator: %s", constraint.GetKind())
		}
		versionedAttr := &admission.VersionedAttributes{
			Attributes:         request,
			VersionedKind:      request.GetKind(),
			VersionedOldObject: request.GetOldObject(),
			VersionedObject:    request.GetObject(),
		}
		response := validator.Validate(ctx, versionedAttr, constraint, celAPI.PerCallLimit)

		enforcementAction, found, err := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
		if err != nil {
			return nil, err
		}
		if !found {
			enforcementAction = apiconstraints.EnforcementActionDeny
		}
		for _, decision := range response.Decisions {
			if decision.Action == validatingadmissionpolicy.ActionDeny {
				results = append(results, &types.Result{
					Target:            target,
					Msg:               decision.Message,
					Constraint:        constraint,
					EnforcementAction: enforcementAction,
				})
			}
		}
		evalElapsedTime := time.Since(evalStartTime)
		if d.gatherStats || (cfg != nil && cfg.StatsEnabled) {
			statsEntries = append(statsEntries,
				&instrumentation.StatsEntry{
					Scope:    instrumentation.ConstraintScope,
					StatsFor: fmt.Sprintf("%s/%s", constraint.GetKind(), constraint.GetName()),
					Stats: []*instrumentation.Stat{
						{
							Name:  runTimeNS,
							Value: uint64(evalElapsedTime.Nanoseconds()),
							Source: instrumentation.Source{
								Type:  instrumentation.EngineSourceType,
								Value: Name,
							},
						},
					},
				})
		}
	}
	return &drivers.QueryResponse{Results: results, StatsEntries: statsEntries}, nil
}

func (d *Driver) Dump(ctx context.Context) (string, error) {
	return "", nil
}

func (d *Driver) GetDescriptionForStat(statName string) (string, error) {
	switch statName {
	case runTimeNS:
		return runTimeNSDescription, nil
	default:
		return "", fmt.Errorf("unknown stat name for K8sNativeValidation: %s", statName)
	}
}

type ARGetter interface {
	GetAdmissionRequest() *admissionv1.AdmissionRequest
}

// FRICTION this wrapper class is excessive. Validator code should define an interface that only requires the methods it needs.
type RequestWrapper struct {
	ar               *admissionv1.AdmissionRequest
	object           runtime.Object
	oldObject        runtime.Object
	operationOptions runtime.Object
}

func NewWrapper(req *admissionv1.AdmissionRequest) (*RequestWrapper, error) {
	var object runtime.Object
	if len(req.Object.Raw) != 0 {
		object = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.Object.Raw, object); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal object", err)
		}
	}

	var oldObject runtime.Object
	if len(req.OldObject.Raw) != 0 {
		oldObject = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.OldObject.Raw, oldObject); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal old object", err)
		}
	}

	// this may be unnecessary, since GetOptions() may not be used by downstream
	// code, but is better than doing this lazily and needing to panic if GetOptions()
	// fails.
	var options runtime.Object
	if len(req.Options.Raw) != 0 {
		options = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.Options.Raw, options); err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal options", err)
		}
	}
	return &RequestWrapper{
		ar:               req,
		object:           object,
		oldObject:        oldObject,
		operationOptions: options,
	}, nil
}

func (w *RequestWrapper) GetName() string {
	return w.ar.Name
}

func (w *RequestWrapper) GetNamespace() string {
	return w.ar.Namespace
}

func (w *RequestWrapper) GetResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    w.ar.Resource.Group,
		Version:  w.ar.Resource.Version,
		Resource: w.ar.Resource.Resource,
	}
}

func (w *RequestWrapper) GetSubresource() string {
	return w.ar.SubResource
}

var opMap = map[admissionv1.Operation]admission.Operation{
	admissionv1.Create:  admission.Create,
	admissionv1.Update:  admission.Update,
	admissionv1.Delete:  admission.Delete,
	admissionv1.Connect: admission.Connect,
}

func (w *RequestWrapper) GetOperation() admission.Operation {
	return opMap[w.ar.Operation]
}

func (w *RequestWrapper) GetOperationOptions() runtime.Object {
	return w.operationOptions
}

func (w *RequestWrapper) IsDryRun() bool {
	if w.ar.DryRun == nil {
		return false
	}
	return *w.ar.DryRun
}

func (w *RequestWrapper) GetObject() runtime.Object {
	return w.object
}

func (w *RequestWrapper) GetOldObject() runtime.Object {
	return w.oldObject
}

func (w *RequestWrapper) GetKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   w.ar.Kind.Group,
		Version: w.ar.Kind.Version,
		Kind:    w.ar.Kind.Kind,
	}
}

func (w *RequestWrapper) GetUserInfo() user.Info {
	extra := map[string][]string{}
	for k := range w.ar.UserInfo.Extra {
		vals := make([]string, len(w.ar.UserInfo.Extra[k]))
		copy(vals, w.ar.UserInfo.Extra[k])
		extra[k] = vals
	}

	return &user.DefaultInfo{
		Name:   w.ar.UserInfo.Username,
		UID:    w.ar.UserInfo.UID,
		Groups: w.ar.UserInfo.Groups,
		Extra:  extra,
	}
}

func (w *RequestWrapper) AddAnnotation(key, value string) error {
	return errors.New("AddAnnotation not implemented")
}

func (w *RequestWrapper) AddAnnotationWithLevel(key, value string, level auditinternal.Level) error {
	return errors.New("AddAnnotationWithLevel not implemented")
}

func (w *RequestWrapper) GetReinvocationContext() admission.ReinvocationContext {
	return nil
}

func convertv1alpha1FailurePolicyTypeTov1FailurePolicyType(policyType *admissionv1alpha1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	if policyType == nil {
		return nil
	}

	var v1FailPolicy admissionregistrationv1.FailurePolicyType
	if *policyType == admissionv1alpha1.Fail {
		v1FailPolicy = admissionregistrationv1.Fail
	} else if *policyType == admissionv1alpha1.Ignore {
		v1FailPolicy = admissionregistrationv1.Ignore
	}
	return &v1FailPolicy
}

func convertv1alpha1Validations(inputValidations []admissionv1alpha1.Validation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		celValidation := validatingadmissionpolicy.ValidationCondition{
			Expression: validation.Expression,
			Message:    validation.Message,
			Reason:     validation.Reason,
		}
		celExpressionAccessor[i] = &celValidation
	}
	return celExpressionAccessor
}

func convertV1Alpha1MessageExpressions(inputValidations []admissionv1alpha1.Validation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		if validation.MessageExpression != "" {
			condition := validatingadmissionpolicy.MessageExpressionCondition{
				MessageExpression: validation.MessageExpression,
			}
			celExpressionAccessor[i] = &condition
		}
	}
	return celExpressionAccessor
}

func convertv1alpha1AuditAnnotations(inputValidations []admissionv1alpha1.AuditAnnotation) []cel.ExpressionAccessor {
	celExpressionAccessor := make([]cel.ExpressionAccessor, len(inputValidations))
	for i, validation := range inputValidations {
		celValidation := validatingadmissionpolicy.AuditAnnotationCondition{
			Key:             validation.Key,
			ValueExpression: validation.ValueExpression,
		}
		celExpressionAccessor[i] = &celValidation
	}
	return celExpressionAccessor
}
