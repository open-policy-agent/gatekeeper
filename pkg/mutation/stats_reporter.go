package mutation

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	mutatorIngestionCountMetricName    = "mutator_ingestion_count"
	mutatorIngestionDurationMetricName = "mutator_ingestion_duration_seconds"
	mutatorsMetricName                 = "mutators"
	mutationSystemIterationsMetricName = "mutation_system_iterations"
)

type MutatorStatus string
type SystemConvergenceStatus string

var (
	mutatorStatusKey                  = tag.MustNewKey("status")
	MutatorStatusActive MutatorStatus = "active"
	MutatorStatusError  MutatorStatus = "error"

	systemConvergenceKey                           = tag.MustNewKey("success")
	SystemConverganceTrue  SystemConvergenceStatus = "true"
	SystemConverganceFalse SystemConvergenceStatus = "false"

	responseTimeInSecM = stats.Float64(
		mutatorIngestionDurationMetricName,
		"The distribution of Mutator ingestion durations",
		stats.UnitSeconds)

	mutatorsM = stats.Int64(
		mutatorsMetricName,
		"The current number of Mutator objects",
		stats.UnitDimensionless)

	systemIterationsM = stats.Int64(
		mutationSystemIterationsMetricName,
		"The distribution of Mutator ingestion durations",
		stats.UnitDimensionless)
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

type reporter struct {
	ctx context.Context
}

func (r *reporter) reportMutatorIngestionRequest(ms MutatorStatus, d time.Duration) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(mutatorStatusKey, string(ms)),
	)
	if err != nil {
		return err
	}

	return r.report(ctx, responseTimeInSecM.M(d.Seconds()))
}

func (r *reporter) reportMutatorsStatus(ms MutatorStatus, n int) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(mutatorStatusKey, string(ms)),
	)
	if err != nil {
		return err
	}

	return r.report(ctx, mutatorsM.M(int64(n)))
}

func (r *reporter) reportIterationConvergence(scs SystemConvergenceStatus, iterations int) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(systemConvergenceKey, string(scs)),
	)
	if err != nil {
		return err
	}

	return r.report(ctx, systemIterationsM.M(int64(iterations)))
}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	return metrics.Record(ctx, m)
}

// newStatsReporter creaters a reporter for webhook metrics
func newStatsReporter() (*reporter, error) {
	ctx, err := tag.New(
		context.Background(),
	)
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

func register() error {
	views := []*view.View{
		{
			Name:        mutatorIngestionCountMetricName,
			Description: "Total number of Mutator ingestion actions",
			Measure:     responseTimeInSecM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		{
			Name: mutatorIngestionDurationMetricName,
			// JULIAN - not sure if I should do this or just inline
			Description: responseTimeInSecM.Description(),
			Measure:     responseTimeInSecM,
			// JULIAN - We'll need to tune this.  I'm not sure if these histogram sections are valid.
			Aggregation: view.Distribution(0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		// JULIAN - We'll probably want to do this in its own call, separate from the request
		// counters.  It's a fundamentally different idea.  It's more of an "audit" of the current
		// state, as opposed to the monitoring of a single request.  That said, it will still end
		// up happening in the same place that the other one is called.
		{
			Name:        mutatorsMetricName,
			Description: "The current number of Mutator objects",
			Measure:     mutatorsM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		{
			Name: mutationSystemIterationsMetricName,
			// JULIAN - not sure if I should do this or just inline
			Description: systemIterationsM.Description(),
			Measure:     systemIterationsM,
			// JULIAN - We'll need to tune this.  I'm not sure if these histogram sections are valid.
			Aggregation: view.Distribution(2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192),
			TagKeys:     []tag.Key{systemConvergenceKey},
		},
	}
	return view.Register(views...)
}
