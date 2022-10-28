package test

import (
	"sort"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type GatorResult struct {
	types.Result

	ViolatingObject *unstructured.Unstructured `json:"violatingObject"`

	// Trace is an explanation of the underlying constraint evaluation.
	// For instance, for OPA based evaluations, the trace is an explanation of the rego query:
	// https://www.openpolicyagent.org/docs/v0.44.0/policy-reference/#tracing
	// NOTE: This is a string pointer to differentiate between an empty ("") trace and an unset one (nil);
	// also for efficiency reasons as traces could be arbitrarily large theoretically.
	Trace *string `json:"trace"`
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
			rr.Trace = resp.Trace
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

// YamlGatorResult is a GatorResult minues a level of indirection on
// the ViolatingObject and with struct tags for yaml marshaling.
type YamlGatorResult struct {
	types.Result
	ViolatingObject map[string]interface{} `yaml:"violatingObject"`
	Trace           *string                `yaml:"trace,flow"`
}

// GetYamlFriendlyResults is a convenience func to remove a level of indirection between
// unstructured.Unstructured and unstructured.Unstructured.Object when calling MarshalYaml.
func GetYamlFriendlyResults(results []*GatorResult) []*YamlGatorResult {
	var yamlResults []*YamlGatorResult

	for _, r := range results {
		yr := &YamlGatorResult{
			Result:          r.Result,
			ViolatingObject: r.ViolatingObject.DeepCopy().Object,
			Trace:           r.Trace,
		}
		yamlResults = append(yamlResults, yr)
	}

	return yamlResults
}
