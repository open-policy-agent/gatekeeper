package test

import (
	"sort"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type GatorResult struct {
	types.Result

	ViolatingObject *unstructured.Unstructured
}

func fromFrameworkResult(frameResult *types.Result, violatingObject *unstructured.Unstructured) *GatorResult {
	gResult := &GatorResult{Result: *frameResult}

	// do a deep copy to detach us from the Constraint Framework's references
	gResult.Constraint = frameResult.Constraint.DeepCopy()

	// set the violating object, which is no longer part of framework results
	gResult.ViolatingObject = violatingObject

	return gResult
}

// Response is a collection of Constraint violations for a particular Target.
// Each Result is for a distinct Constraint.
type GatorResponse struct {
	Trace   *string
	Target  string
	Results []*GatorResult
}

type GatorResponses struct {
	ByTarget map[string]*GatorResponse
	Handled  map[string]bool
}

func (r *GatorResponses) Results() []*GatorResult {
	if r == nil {
		return nil
	}

	var res []*GatorResult
	for target, resp := range r.ByTarget {
		for _, rr := range resp.Results {
			rr.Target = target
			res = append(res, rr)
		}
	}

	// Make results more (but not completely) deterministic.
	// After we shard Rego compilation environments, we will be able to tie
	// responses to individual constraints. This is a stopgap to make tests easier
	// to write until then.
	sort.Slice(res, func(i, j int) bool {
		if res[i].EnforcementAction != res[j].EnforcementAction {
			return res[i].EnforcementAction < res[j].EnforcementAction
		}
		return res[i].Msg < res[j].Msg
	})

	return res
}

type testOptions struct {
	referentialData bool
}

func defaultTestOptions() *testOptions {
	return &testOptions{
		referentialData: true,
	}
}

func mutateTestOptions(ops *testOptions, mutators ...TestOptionMutator) {
	for _, m := range mutators {
		m(ops)
	}
}

type TestOptionMutator func(*testOptions)

var DisableReferentialData TestOptionMutator = func(ops *testOptions) {
	ops.referentialData = false
}
