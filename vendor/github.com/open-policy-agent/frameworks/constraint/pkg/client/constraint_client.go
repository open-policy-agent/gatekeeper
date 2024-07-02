package client

import (
	"fmt"

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

	// enforcementActionsForEP stores precompiled enforcement actions for each enforcement point.
	enforcementActionsForEP map[string][]string
}

func (c *constraintClient) getConstraint() *unstructured.Unstructured {
	return c.constraint.DeepCopy()
}

func (c *constraintClient) matches(target string, review interface{}, sourceEPs ...string) *constraintMatchResult {
	matcher, found := c.matchers[target]
	if !found {
		return nil
	}

	var actions []string
	for _, ep := range sourceEPs {
		if acts, found := c.enforcementActionsForEP[ep]; found {
			actions = append(actions, acts...)
		}
	}

	if len(actions) == 0 {
		return nil
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
			constraint:         c.constraint,
			error:              fmt.Errorf("%w: %v", errors.ErrAutoreject, err),
			enforcementActions: actions,
		}
	case matches:
		// Fill in Constraint, so we can pass it to the Driver to run.
		return &constraintMatchResult{
			constraint:         c.constraint,
			enforcementActions: actions,
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
	enforcementActions []string
	// error is a problem encountered while attempting to run the Constraint's
	// Matcher.
	error error
}

func (r *constraintMatchResult) ToResult() *types.Result {
	return &types.Result{
		Msg:               r.error.Error(),
		Constraint:        r.constraint,
		EnforcementAction: r.enforcementActions,
	}
}
