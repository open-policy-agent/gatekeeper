package mutation

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	mutationSystemIterationsMetricName = "mutation_system_iterations"
	systemConvergenceKey               = "success"
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

var systemIterationsM metric.Int64Histogram

func init() {
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")

	systemIterationsM, err = meter.Int64Histogram(
		mutationSystemIterationsMetricName,
		metric.WithDescription("The distribution of Mutation System iterations before convergence"),
	)
	if err != nil {
		panic(err)
	}

	view.Register(sdkmetric.NewView(
		sdkmetric.Instrument{Name: mutationSystemIterationsMetricName},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 20, 50, 100, 200, 500},
			},
		},
	))
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
	systemIterationsM.Record(context.Background(), int64(iterations), metric.WithAttributes(attribute.String(systemConvergenceKey, string(scs))))
	return nil
}
