package drivers

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

// QueryResponse encapsulates the values returned on Query:
// - Results includes a Result for each violated Constraint.
// - Trace is the evaluation trace on Query if specified in query options or enabled at Driver creation.
// - StatsEntries include any Stats that the engine gathered on Query.
type QueryResponse struct {
	Results      []*types.Result
	Trace        *string
	StatsEntries []*instrumentation.StatsEntry
}
