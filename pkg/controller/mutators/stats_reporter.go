package mutators

import (
	"context"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	mutatorIngestionCountMetricName     = "mutator_ingestion_count"
	mutatorIngestionDurationMetricName  = "mutator_ingestion_duration_seconds"
	mutatorsMetricName                  = "mutators"
	mutatorsConflictingCountMetricsName = "mutator_conflicting_count"
	statusKey                           = "status"
)

var (
	mutatorIngestionCountM metric.Int64Counter
	responseTimeInSecM     metric.Float64Histogram
)

// MutatorIngestionStatus defines the outcomes of an attempt to add a Mutator to the mutation System.
type MutatorIngestionStatus string

const (
	// MutatorStatusActive denotes a successfully ingested mutator, ready to mutate objects.
	MutatorStatusActive MutatorIngestionStatus = "active"
	// MutatorStatusError denotes a mutator that failed to ingest.
	MutatorStatusError MutatorIngestionStatus = "error"
)

// StatsReporter reports mutator-related controller metrics.
type StatsReporter interface {
	ReportMutatorIngestionRequest(ms MutatorIngestionStatus, d time.Duration) error
	ReportMutatorsStatus(ms MutatorIngestionStatus, n int) error
	ReportMutatorsInConflict(n int) error
}

// reporter implements StatsReporter interface.
type reporter struct {
	mu                  sync.RWMutex
	mutatorStatusReport map[MutatorIngestionStatus]int
	mutatorsInConflict  int
}

// NewStatsReporter creates a reporter for webhook metrics.
func NewStatsReporter() StatsReporter {
	r := &reporter{}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")

	mutatorIngestionCountM, err = meter.Int64Counter(
		mutatorIngestionCountMetricName,
		metric.WithDescription("Total number of Mutator ingestion actions"),
	)
	if err != nil {
		panic(err)
	}

	responseTimeInSecM, err = meter.Float64Histogram(
		mutatorIngestionDurationMetricName,
		metric.WithDescription("The distribution of Mutator ingestion durations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.Int64ObservableGauge(
		mutatorsMetricName,
		metric.WithDescription("The current number of Mutator objects"),
		metric.WithInt64Callback(r.observeMutatorsStatus),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.Int64ObservableGauge(
		mutatorsConflictingCountMetricsName,
		metric.WithDescription("The current number of conflicting Mutator objects"),
		metric.WithInt64Callback(r.observeMutatorsInConflict),
	)
	if err != nil {
		panic(err)
	}

	view.Register(sdkmetric.NewView(
		sdkmetric.Instrument{Name: mutatorIngestionDurationMetricName},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05},
			},
		},
	))
	return r
}

// ReportMutatorIngestionRequest reports both the action of a mutator ingestion and the time
// required for this request to complete.  The outcome of the ingestion attempt is recorded via the
// status argument.
func (r *reporter) ReportMutatorIngestionRequest(ms MutatorIngestionStatus, d time.Duration) error {
	responseTimeInSecM.Record(context.Background(), d.Seconds(), metric.WithAttributes(attribute.String(statusKey, string(ms))))
	mutatorIngestionCountM.Add(context.Background(), 1, metric.WithAttributes(attribute.String(statusKey, string(ms))))
	return nil
}

// ReportMutatorsStatus reports the number of mutators of a specific status that are present in the
// mutation System.
func (r *reporter) ReportMutatorsStatus(ms MutatorIngestionStatus, n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.mutatorStatusReport == nil {
		r.mutatorStatusReport = make(map[MutatorIngestionStatus]int)
	}
	r.mutatorStatusReport[ms] = n
	return nil
}

func (r *reporter) observeMutatorsStatus(_ context.Context, observer metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for status, count := range r.mutatorStatusReport {
		observer.Observe(int64(count), metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}

// ReportMutatorsInConflict reports the number of mutators that have schema
// conflicts with other mutators in the mutation system.
func (r *reporter) ReportMutatorsInConflict(n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mutatorsInConflict = n
	return nil
}

func (r *reporter) observeMutatorsInConflict(_ context.Context, observer metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	observer.Observe(int64(r.mutatorsInConflict))
	return nil
}
