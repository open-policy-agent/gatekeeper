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

var (
	MutatorStatusActive MutatorStatus = "active"
	MutatorStatusError  MutatorStatus = "error"

	// JULIAN - This may need to just be "status"
	mutatorStatusKey = tag.MustNewKey("mutator_status")
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

type reporter struct {
	ctx context.Context
}

func (r *reporter) reportMutatorIngestion(ms MutatorStatus, d time.Duration, mutators int) error {
	return nil
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
	views := []*view.View{}
	return view.Register(views...)
}
