package types

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	TracingDisabledHeader = "Trace: TRACING DISABLED"
)

type Result struct {
	// Target is the target this violation is for.
	Target string `json:"target"`

	Msg string `json:"msg,omitempty"`

	// Metadata includes the contents of `details` from the Rego rule signature
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// The constraint that was violated
	Constraint *unstructured.Unstructured `json:"constraint,omitempty"`

	// The enforcement action of the constraint
	EnforcementAction string `json:"enforcementAction,omitempty"`

	// The scoped actions of the constraint
	ScopedEnforcementActions []string `json:"scopedActions,omitempty"`
}

// Response is a collection of Constraint violations for a particular Target.
// Each Result represents a violation for a distinct Constraint.
type Response struct {
	Trace   *string
	Target  string
	Results []*Result
}

func (r *Response) AddResult(results *Result) {
	r.Results = append(r.Results, results)
}

// Sort sorts the Results in Response lexicographically first by the Constraint
// Kind, and then by Constraint Name.
func (r *Response) Sort() {
	// Since Constraints are uniquely identified by Kind and Name, this guarantees
	// a stable sort when each Result is for a different Constraint.
	sort.Slice(r.Results, func(i, j int) bool {
		resultI := r.Results[i]
		resultJ := r.Results[j]

		kindI := resultI.Constraint.GetKind()
		kindJ := resultJ.Constraint.GetKind()
		if kindI != kindJ {
			return kindI < kindJ
		}

		nameI := resultI.Constraint.GetName()
		nameJ := resultJ.Constraint.GetName()
		return nameI < nameJ
	})
}

func (r *Response) TraceDump() string {
	b := &strings.Builder{}
	_, _ = fmt.Fprintf(b, "Target: %s\n", r.Target)
	if r.Trace == nil {
		if r.Results != nil {
			// only say "Trace: TRACING DISABLED" if there are results
			// otherwise, we risk to confuse consumers who see the msg
			// and think that evaluation did not happen.

			// Note that if there were NO violating results AND the trace
			// was turned on, then r.Trace != nil.
			b.WriteString(TracingDisabledHeader)
			b.WriteString("\n\n")
		}
	} else {
		_, _ = fmt.Fprintf(b, "Trace:\n%s\n\n", *r.Trace)
	}
	for i, result := range r.Results {
		_, _ = fmt.Fprintf(b, "Result(%d):\n%s\n\n", i, spew.Sdump(result))
	}
	return b.String()
}

func NewResponses() *Responses {
	return &Responses{
		ByTarget:     make(map[string]*Response),
		Handled:      make(map[string]bool),
		StatsEntries: make([]*instrumentation.StatsEntry, 0),
	}
}

type Responses struct {
	ByTarget     map[string]*Response
	Handled      map[string]bool
	StatsEntries []*instrumentation.StatsEntry
}

func (r *Responses) Results() []*Result {
	if r == nil {
		return nil
	}

	var res []*Result
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

func (r *Responses) HandledCount() int {
	if r == nil {
		return 0
	}

	c := 0
	for _, h := range r.Handled {
		if h {
			c++
		}
	}

	return c
}

func (r *Responses) TraceDump() string {
	b := &strings.Builder{}
	for _, resp := range r.ByTarget {
		_, _ = fmt.Fprintln(b, resp.TraceDump())
		_, _ = fmt.Fprintln(b, "")
	}
	return b.String()
}
