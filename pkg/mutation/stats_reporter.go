package mutation

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	mutationSystemIterationsMetricName = "mutation_system_iterations"
)

// SystemConvergenceStatus defines the outcomes of the attempted mutation of an object by the
// mutation System. The System is meant to converge on a fully mutated object.
type SystemConvergenceStatus string

const (
	// SystemConvergenceTrue denotes a successfully converged mutation system request.
	SystemConvergenceTrue SystemConvergenceStatus = "true"
	// SystemConvergenceFalse denotes an unsuccessfully converged mutation system request.
	SystemConvergenceFalse SystemConvergenceStatus = "false"
)

var (
	systemConvergenceKey = tag.MustNewKey("success")

	systemIterationsM = stats.Int64(
		mutationSystemIterationsMetricName,
		"The distribution of Mutation System iterations before convergence",
		stats.UnitDimensionless)
)

func init() {
	views := []*view.View{
		{
			Name:        mutationSystemIterationsMetricName,
			Description: systemIterationsM.Description(),
			Measure:     systemIterationsM,
			Aggregation: view.Distribution(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 20, 50, 100, 200, 500),
			TagKeys:     []tag.Key{systemConvergenceKey},
		},
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
}

// StatsReporter reports mutator-related metrics.
type StatsReporter interface {
	ReportIterationConvergence(scs SystemConvergenceStatus, iterations int) error
}

// reporter implements StatsReporter interface.
type reporter struct{}

// NewStatsReporter creates a reporter for webhook metrics.
func NewStatsReporter() StatsReporter {
	return &reporter{}
}

// ReportIterationConvergence reports the success or failure of the mutation system to converge.
// It also records the number of system iterations that were required to reach this end.
func (r *reporter) ReportIterationConvergence(scs SystemConvergenceStatus, iterations int) error {
	// No need for an actual Context.
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(systemConvergenceKey, string(scs)),
	)
	if err != nil {
		return err
	}

	return metrics.Record(ctx, systemIterationsM.M(int64(iterations)))
}
