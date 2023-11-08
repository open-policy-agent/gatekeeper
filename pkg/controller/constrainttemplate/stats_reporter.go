package constrainttemplate

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ctMetricName   = "constraint_templates"
	ingestCount    = "constraint_template_ingestion_count"
	ingestDuration = "constraint_template_ingestion_duration_seconds"
	statusKey      = "status"

	ctDesc = "Number of observed constraint templates"
)

var (
	ctM             metric.Int64ObservableGauge
	ingestCountM    metric.Int64Counter
	ingestDurationM metric.Float64Histogram
	meter           metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")
	ctM, err = meter.Int64ObservableGauge(
		ctMetricName,
		metric.WithDescription(ctDesc),
	)

	if err != nil {
		panic(err)
	}

	ingestCountM, err = meter.Int64Counter(
		ingestCount,
		metric.WithDescription("Total number of constraint template ingestion actions"),
	)
	if err != nil {
		panic(err)
	}

	ingestDurationM, err = meter.Float64Histogram(
		ingestDuration,
		metric.WithDescription("Distribution of how long it took to ingest a constraint template in seconds"),
	)
	if err != nil {
		panic(err)
	}
	view.Register(sdkmetric.NewView(
		sdkmetric.Instrument{Name: ingestDuration},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09, 0.1, 0.2, 0.3, 0.4, 0.5, 1, 2, 3, 4, 5},
			},
		},
	))
}

func (r *reporter) reportIngestDuration(ctx context.Context, status metrics.Status, d time.Duration) error {
	ingestDurationM.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String(statusKey, string(status))))
	ingestCountM.Add(ctx, 1, metric.WithAttributes(attribute.String(statusKey, string(status))))
	return nil
}

// newStatsReporter creates a reporter for watch metrics.
func newStatsReporter() *reporter {
	reg := &ctRegistry{cache: make(map[types.NamespacedName]metrics.Status)}
	return &reporter{registry: reg}
}

type reporter struct {
	registry *ctRegistry
}

type ctRegistry struct {
	cache map[types.NamespacedName]metrics.Status
	dirty bool
}

func (r *ctRegistry) add(key types.NamespacedName, status metrics.Status) {
	v, ok := r.cache[key]
	if ok && v == status {
		return
	}
	r.cache[key] = status
	r.dirty = true
}

func (r *ctRegistry) remove(key types.NamespacedName) {
	if _, ok := r.cache[key]; !ok {
		return
	}
	delete(r.cache, key)
	r.dirty = true
}

func (r *ctRegistry) registerCallback() error {
	_, err := meter.RegisterCallback(r.observeCTM, ctM)
	return err
}

func (r *ctRegistry) observeCTM(_ context.Context, o metric.Observer) error {
	totals := make(map[metrics.Status]int64)
	for _, status := range r.cache {
		totals[status]++
	}
	for _, status := range metrics.AllStatuses {
		o.ObserveInt64(ctM, totals[status], metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}
