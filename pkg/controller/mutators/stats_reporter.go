package mutators

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	mutatorIngestionCountMetricName     = "mutator_ingestion_count"
	mutatorIngestionDurationMetricName  = "mutator_ingestion_duration_seconds"
	mutatorsMetricName                  = "mutators"
	mutatorsConflictingCountMetricsName = "mutator_conflicting_count"
)

// MutatorIngestionStatus defines the outcomes of an attempt to add a Mutator to the mutation System.
type MutatorIngestionStatus string

const (
	// MutatorStatusActive denotes a successfully ingested mutator, ready to mutate objects.
	MutatorStatusActive MutatorIngestionStatus = "active"
	// MutatorStatusError denotes a mutator that failed to ingest.
	MutatorStatusError MutatorIngestionStatus = "error"
)

var (
	mutatorStatusKey = tag.MustNewKey("status")

	responseTimeInSecM = stats.Float64(
		mutatorIngestionDurationMetricName,
		"The distribution of Mutator ingestion durations",
		stats.UnitSeconds)

	mutatorsM = stats.Int64(
		mutatorsMetricName,
		"The current number of Mutator objects",
		stats.UnitDimensionless)

	conflictingMutatorsM = stats.Int64(
		mutatorsConflictingCountMetricsName,
		"The current number of conflicting Mutator objects",
		stats.UnitDimensionless)
)

func init() {
	views := []*view.View{
		{
			Name:        mutatorIngestionCountMetricName,
			Description: "Total number of Mutator ingestion actions",
			Measure:     responseTimeInSecM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		{
			Name:        mutatorIngestionDurationMetricName,
			Description: responseTimeInSecM.Description(),
			Measure:     responseTimeInSecM,
			Aggregation: view.Distribution(0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		{
			Name:        mutatorsMetricName,
			Description: mutatorsM.Description(),
			Measure:     mutatorsM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{mutatorStatusKey},
		},
		{
			Name:        mutatorsConflictingCountMetricsName,
			Description: conflictingMutatorsM.Description(),
			Measure:     conflictingMutatorsM,
			Aggregation: view.LastValue(),
		},
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
}

// StatsReporter reports mutator-related controller metrics.
type StatsReporter interface {
	ReportMutatorIngestionRequest(ms MutatorIngestionStatus, d time.Duration) error
	ReportMutatorsStatus(ms MutatorIngestionStatus, n int) error
	ReportMutatorsInConflict(n int) error
}

// reporter implements StatsReporter interface.
type reporter struct{}

// NewStatsReporter creates a reporter for webhook metrics.
func NewStatsReporter() StatsReporter {
	return &reporter{}
}

// ReportMutatorIngestionRequest reports both the action of a mutator ingestion and the time
// required for this request to complete.  The outcome of the ingestion attempt is recorded via the
// status argument.
func (r *reporter) ReportMutatorIngestionRequest(ms MutatorIngestionStatus, d time.Duration) error {
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(mutatorStatusKey, string(ms)),
	)
	if err != nil {
		return err
	}

	return metrics.Record(ctx, responseTimeInSecM.M(d.Seconds()))
}

// ReportMutatorsStatus reports the number of mutators of a specific status that are present in the
// mutation System.
func (r *reporter) ReportMutatorsStatus(ms MutatorIngestionStatus, n int) error {
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(mutatorStatusKey, string(ms)),
	)
	if err != nil {
		return err
	}

	return metrics.Record(ctx, mutatorsM.M(int64(n)))
}

// ReportMutatorsInConflict reports the number of mutators that have schema
// conflicts with other mutators in the mutation system.
func (r *reporter) ReportMutatorsInConflict(n int) error {
	return metrics.Record(context.Background(), conflictingMutatorsM.M(int64(n)))
}
