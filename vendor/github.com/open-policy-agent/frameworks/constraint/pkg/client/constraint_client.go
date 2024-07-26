package client

import (
	"fmt"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// constraintClient handler per-Constraint operations.
//
// Not threadsafe.
type constraintClient struct {
	// constraint is a copy of the original Constraint added to Client.
	constraint *unstructured.Unstructured

	// matchers are the per-target Matchers for this Constraint.
	matchers map[string]constraints.Matcher

	// enforcementAction is what should be done if the Constraint is violated or
	// fails to run on a review.
	enforcementAction string

	// enforcementActionsForEP stores precompiled enforcement actions for each enforcement point.
	enforcementActionsForEP map[string][]string
}

func (c *constraintClient) getConstraint() *unstructured.Unstructured {
	return c.constraint.DeepCopy()
}

func (c *constraintClient) matches(target string, review interface{}, enforcementPoints ...string) *constraintMatchResult {
	matcher, found := c.matchers[target]
	if !found {
		return nil
	}

	// Initialize a map to track unique enforcement actions
	enforcementActions := make(map[string]bool)
	if apiconstraints.IsEnforcementActionScoped(c.enforcementAction) {
		// Iterate over the provided source enforcement points (EPs)
		for _, ep := range enforcementPoints {
			var actions []string
			if ep == apiconstraints.AllEnforcementPoints {
				// If the EP is "*", aggregate actions from all EPs
				for _, acts := range c.enforcementActionsForEP {
					actions = append(actions, acts...)
				}
			}
			// Check if there are predefined actions for the current EP
			if acts, found := c.enforcementActionsForEP[ep]; found {
				actions = append(actions, acts...) // Use the predefined actions if found
			}
			// Mark each action as true in the map to ensure uniqueness
			for _, act := range actions {
				enforcementActions[act] = true
			}
		}
	}
	// If enforcement action is scoped, constraint does not include enforcement point that needs to be enforced then there is no action to be taken.
	// If no enforcement actions are found, return nil
	if len(enforcementActions) == 0 && apiconstraints.IsEnforcementActionScoped(c.enforcementAction) {
		return nil
	}

	var actions []string
	for action := range enforcementActions {
		actions = append(actions, action)
	}
	matches, err := matcher.Match(review)

	// We avoid DeepCopying the Constraint out of the Client cache here, only
	// DeepCopying when we're about to return the Constraint to the user in
	// Driver.ToResults. Preemptive DeepCopying is expensive.
	// This does mean Driver must take care to never modify the Constraints it
	// is passed.
	switch {
	case err != nil:
		// Fill in the Constraint's enforcementAction since we were unable to
		// determine if the Constraint matched, so we assume it violated the
		// Constraint.
		return &constraintMatchResult{
			constraint:               c.constraint,
			error:                    fmt.Errorf("%w: %v", errors.ErrAutoreject, err),
			enforcementAction:        c.enforcementAction,
			scopedEnforcementActions: actions,
		}
	case matches:
		// Fill in Constraint, so we can pass it to the Driver to run.
		return &constraintMatchResult{
			constraint:               c.constraint,
			enforcementAction:        c.enforcementAction,
			scopedEnforcementActions: actions,
		}
	default:
		// No match and no error, so no need to record a result.
		return nil
	}
}

type constraintMatchResult struct {
	// constraint is a pointer to the Constraint. Not safe for modification.
	constraint *unstructured.Unstructured
	// enforcementAction, if specified, is the immediate action to take.
	// Only filled in if error is non-nil.
	enforcementAction string
	// scopedEnforcementActions are action to take for specific enforcement point.
	// Only filled in if error is non-nil.
	scopedEnforcementActions []string
	// error is a problem encountered while attempting to run the Constraint's
	// Matcher.
	error error
}

func (r *constraintMatchResult) ToResult() *types.Result {
	return &types.Result{
		Msg:                      r.error.Error(),
		Constraint:               r.constraint,
		EnforcementAction:        r.enforcementAction,
		ScopedEnforcementActions: r.scopedEnforcementActions,
	}
}
