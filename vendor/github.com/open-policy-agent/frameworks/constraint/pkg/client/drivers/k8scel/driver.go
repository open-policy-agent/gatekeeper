package k8scel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	pSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/transform"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/storage"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	celAPI "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/environment"
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
	runTimeNS            = "runTimeNS"
	runTimeNSDescription = "the number of nanoseconds it took to evaluate the constraint"
)

var _ drivers.Driver = &Driver{}

type Driver struct {
	mux         sync.RWMutex
	validators  map[string]*validatorWrapper
	gatherStats bool
}

type validatorWrapper struct {
	validator validating.Validator
}

func (d *Driver) Name() string {
	return pSchema.Name
}

func (d *Driver) AddTemplate(_ context.Context, ct *templates.ConstraintTemplate) error {
	source, err := pSchema.GetSourceFromTemplate(ct)
	if err != nil {
		return err
	}

	// FRICTION: Note that compilation errors are possible, but we cannot introspect to see whether any
	// occurred
	celVars := cel.OptionalVariableDeclarations{}

	// We don't want to have access to parameters for anything other than driver-defined logic, so we
	// can keep the user from accessing the full constraint schema.
	celVarsWithParameters := cel.OptionalVariableDeclarations{HasParams: true}

	vapVars := transform.AllVariablesCEL()
	vapVarsSuffix, err := source.GetVariables()
	if err != nil {
		return err
	}
	vapVars = append(vapVars, vapVarsSuffix...)
	// Defaulting to true in MustBaseEnvSet to enforce strict cost calculation for CEL, following k8s.
	filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion(), transform.StrictCost))
	if err != nil {
		return err
	}
	filterCompiler.CompileAndStoreVariables(vapVars, celVarsWithParameters, environment.StoredExpressions)

	failurePolicy, err := source.GetFailurePolicy()
	if err != nil {
		return err
	}

	matchAccessors, err := source.GetMatchConditions()
	if err != nil {
		return err
	}
	matcher := matchconditions.NewMatcher(filterCompiler.Compile(matchAccessors, celVars, environment.StoredExpressions), failurePolicy, "validatingadmissionpolicy", "vap-matcher", ct.GetName())

	validationAccessors, err := source.GetValidations()
	if err != nil {
		return err
	}

	messageAccessors, err := source.GetMessageExpressions()
	if err != nil {
		return err
	}

	validator := validating.NewValidator(
		filterCompiler.Compile(validationAccessors, celVars, environment.StoredExpressions),
		matcher,
		filterCompiler.Compile(nil, celVars, environment.StoredExpressions),
		filterCompiler.Compile(messageAccessors, celVars, environment.StoredExpressions),
		failurePolicy,
	)

	d.mux.Lock()
	defer d.mux.Unlock()
	d.validators[ct.GetName()] = &validatorWrapper{
		validator: validator,
	}
	return nil
}

func (d *Driver) RemoveTemplate(_ context.Context, ct *templates.ConstraintTemplate) error {
	d.mux.Lock()
	defer d.mux.Unlock()
	delete(d.validators, ct.GetName())
	return nil
}

func (d *Driver) AddConstraint(_ context.Context, _ *unstructured.Unstructured) error {
	return nil
}

func (d *Driver) RemoveConstraint(_ context.Context, _ *unstructured.Unstructured) error {
	return nil
}

func (d *Driver) AddData(_ context.Context, _ string, _ storage.Path, _ interface{}) error {
	return nil
}

func (d *Driver) RemoveData(_ context.Context, _ string, _ storage.Path) error {
	return nil
}

func (d *Driver) Query(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...reviews.ReviewOpt) (*drivers.QueryResponse, error) {
	cfg := &reviews.ReviewCfg{}
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
	// Gatekeeper sets Object to oldObject on DELETE requests
	// however, Kubernetes does not do this for ValidatingAdmissionPolicy.
	// in order for evaluation in both environments to behave identically,
	// we must be sure that Object is unset on DELETE. Users who need
	// the "if DELETE Object == OldObject" behavior should use the
	// `variables.anyObject` variable instead.
	if aRequest.Operation == admissionv1.Delete {
		aRequest.Object = runtime.RawExtension{}
	}
	versionedAttr, err := transform.RequestToVersionedAttributes(aRequest)
	if err != nil {
		return nil, err
	}

	results := []*types.Result{}

	for _, constraint := range constraints {
		evalStartTime := time.Now()
		// template name is the lowercase of its kind
		wrappedValidator := d.validators[strings.ToLower(constraint.GetKind())]
		if wrappedValidator == nil {
			return nil, fmt.Errorf("unknown constraint template validator: %s", constraint.GetKind())
		}

		validator := wrappedValidator.validator

		// this should never happen, but best not to panic if the pointer is ever nil.
		if validator == nil {
			return nil, fmt.Errorf("nil validator for constraint template %v", strings.ToLower(constraint.GetKind()))
		}

		// TODO: should namespace be made available, if possible? Generally that context should be present
		response := validator.Validate(ctx, versionedAttr.GetResource(), versionedAttr, constraint, nil, celAPI.PerCallLimit, nil)

		for _, decision := range response.Decisions {
			if decision.Action == validating.ActionDeny {
				results = append(results, &types.Result{
					Target:     target,
					Msg:        decision.Message,
					Constraint: constraint,
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
								Value: pSchema.Name,
							},
						},
					},
				})
		}
	}
	return &drivers.QueryResponse{Results: results, StatsEntries: statsEntries}, nil
}

func (d *Driver) Dump(_ context.Context) (string, error) {
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

type IsAdmissionGetter interface {
	IsAdmissionRequest() bool
}
