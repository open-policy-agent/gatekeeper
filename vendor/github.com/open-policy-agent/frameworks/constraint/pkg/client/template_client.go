package client

import (
	"fmt"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/crds"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	constraintlib "github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// templateClient handles per-ConstraintTemplate operations.
//
// Not threadsafe.
type templateClient struct {
	// targets are the Targets which this Template is executed for.
	targets []handler.TargetHandler

	// template is a copy of the original ConstraintTemplate added to Client.
	template *templates.ConstraintTemplate

	// constraints are all currently-known Constraints for this Template.
	constraints map[string]*constraintClient

	// crd is a cache of the generated CustomResourceDefinition generated from
	// this Template. This is used to validate incoming Constraints before adding
	// them.
	crd *apiextensions.CustomResourceDefinition

	// if, for some reason, there was an error adding a pre-cached constraint after
	// a driver switch, AddTemplate returns an error. We should preserve that state
	// so that we know a constraint replay should be attempted the next time AddTemplate
	// is called.
	needsConstraintReplay bool

	// activeDrivers keeps track of drivers that are in an ambiguous state due to a failed
	// cross-driver update. This allows us to clean up stale state on old drivers.
	activeDrivers map[string]bool
}

func newTemplateClient() *templateClient {
	return &templateClient{
		constraints:   make(map[string]*constraintClient),
		activeDrivers: make(map[string]bool),
	}
}

func (e *templateClient) ValidateConstraint(constraint *unstructured.Unstructured) error {
	for _, target := range e.targets {
		err := target.ValidateConstraint(constraint)
		if err != nil {
			return fmt.Errorf("%w: %v", apiconstraints.ErrInvalidConstraint, err)
		}
	}

	return crds.ValidateCR(constraint, e.crd)
}

// ApplyDefaultParams will apply any default parameters defined in the CRD of the constraint's
// corresponding template.
// Assumes ValidateConstraint() is called so the constraint is a valid CRD.
func (e *templateClient) ApplyDefaultParams(constraint *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	structural, err := schema.NewStructural(e.crd.Spec.Validation.OpenAPIV3Schema)
	if err != nil {
		return nil, err
	}

	defaulting.Default(constraint.Object, structural)
	return constraint, nil
}

func (e *templateClient) getTemplate() *templates.ConstraintTemplate {
	return e.template.DeepCopy()
}

func (e *templateClient) Update(templ *templates.ConstraintTemplate, crd *apiextensions.CustomResourceDefinition, targets ...handler.TargetHandler) {
	cpy := templ.DeepCopy()
	cpy.Status = templates.ConstraintTemplateStatus{}

	// Updating e.template must happen after any operations which may fail have
	// completed successfully. This ensures the SemanticEqual exit-early is not
	// triggered unless the Template was previously successfully added.
	e.template = cpy
	e.crd = crd
	e.targets = targets
}

// AddConstraint adds the Constraint to the Template.
// Returns true and no error if the Constraint was changed successfully.
// Returns false and no error if the Constraint was not updated due to being
// identical to the stored version.
func (e *templateClient) AddConstraint(constraint *unstructured.Unstructured, enforcementPoints []string) (bool, error) {
	enforcementActionsForEPs := make(map[string][]string)
	enforcementAction, err := apiconstraints.GetEnforcementAction(constraint)
	if err != nil {
		return false, err
	}

	if apiconstraints.IsEnforcementActionScoped(enforcementAction) {
		enforcementActionsForEPs, err = apiconstraints.GetEnforcementActionsForEP(constraint, enforcementPoints)
		if err != nil {
			return false, err
		}
	} else {
		for _, ep := range enforcementPoints {
			enforcementActionsForEPs[ep] = append(enforcementActionsForEPs[ep], enforcementAction)
		}
	}

	// Compare with the already-existing Constraint.
	// If identical, exit early.
	cached, found := e.constraints[constraint.GetName()]
	if found && constraintlib.SemanticEqual(cached.constraint, constraint) {
		return false, nil
	}

	matchers, err := makeMatchers(e.targets, constraint)
	if err != nil {
		return false, err
	}

	cpy := constraint.DeepCopy()
	delete(cpy.Object, statusField)

	e.constraints[constraint.GetName()] = &constraintClient{
		constraint:              cpy,
		matchers:                matchers,
		enforcementAction:       enforcementAction,
		enforcementActionsForEP: enforcementActionsForEPs,
	}

	return true, nil
}

// GetConstraint returns the Constraint with name for this Template.
func (e *templateClient) GetConstraint(name string) (*unstructured.Unstructured, error) {
	constraint, found := e.constraints[name]
	if !found {
		kind := e.template.Spec.CRD.Spec.Names.Kind
		return nil, fmt.Errorf("%w: %q %q", ErrMissingConstraint, kind, name)
	}

	return constraint.getConstraint(), nil
}

func (e *templateClient) RemoveConstraint(name string) {
	delete(e.constraints, name)
}

// Matches returns a map from Constraint names to the results of running Matchers
// against the passed review.
//
// ignoredTargets specifies the targets whose matchers to not run.
func (e *templateClient) Matches(target string, review interface{}, enforcementPoints []string) map[string]constraintMatchResult {
	result := make(map[string]constraintMatchResult)

	for name, constraint := range e.constraints {
		cResult := constraint.matches(target, review, enforcementPoints...)
		if cResult != nil {
			result[name] = *cResult
		}
	}

	return result
}

func makeMatchers(targets []handler.TargetHandler, constraint *unstructured.Unstructured) (map[string]constraintlib.Matcher, error) {
	result := make(map[string]constraintlib.Matcher)
	errs := clienterrors.ErrorMap{}

	for _, target := range targets {
		name := target.GetName()
		matcher, err := target.ToMatcher(constraint)
		if err != nil {
			errs.Add(name, fmt.Errorf("%w: %v", apiconstraints.ErrInvalidConstraint, err))
		}

		result[name] = matcher
	}

	if len(errs) > 0 {
		return nil, &errs
	}

	return result, nil
}
