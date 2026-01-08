package constrainttemplate

import (
	"context"
	"sync"
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
	ctMetricName = "constraint_templates"
	// celCTMetricName is a separate metric for backward compatibility with existing metrics. Together with ctMetricName, it allows to derive the number of non-CEL constraint templates.
	// Example: rego_only_count = constraint_templates - constraint_templates_with_cel
	celCTMetricName = "constraint_templates_with_cel"
	vapMetricName   = "validating_admission_policies"
	ingestCount     = "constraint_template_ingestion_count"
	ingestDuration  = "constraint_template_ingestion_duration_seconds"
	statusKey       = "status"

	ctDesc    = "Number of observed constraint templates"
	celCTDesc = "Number of constraint templates with CEL engine"
)

var (
	ingestCountM    metric.Int64Counter
	ingestDurationM metric.Float64Histogram
)

func init() {
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
	var err error
	reg := &ctRegistry{cache: make(map[types.NamespacedName]metrics.Status)}
	r := &reporter{registry: reg, vapRegistry: metrics.NewVAPStatusRegistry(), celRegistry: newCelRegistry()}
	meter := otel.GetMeterProvider().Meter("gatekeeper")
	_, err = meter.Int64ObservableGauge(
		ctMetricName,
		metric.WithDescription(ctDesc),
		metric.WithInt64Callback(r.observeCTM),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.Int64ObservableGauge(
		celCTMetricName,
		metric.WithDescription(celCTDesc),
		metric.WithInt64Callback(r.observeCelCTM),
	)
	if err != nil {
		panic(err)
	}

	_, err = meter.Int64ObservableGauge(
		vapMetricName,
		metric.WithDescription("Number of ValidatingAdmissionPolicy resources by generation status (active = successfully generated, error = generation failed)"),
		metric.WithInt64Callback(r.observeVAP),
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
	return r
}

type reporter struct {
	mu          sync.RWMutex
	ctReport    map[metrics.Status]int64
	registry    *ctRegistry
	vapRegistry *metrics.VAPStatusRegistry
	celRegistry *celRegistry
}

type celRegistry struct {
	mu    sync.RWMutex
	cache map[types.NamespacedName]bool
}

func newCelRegistry() *celRegistry {
	return &celRegistry{cache: make(map[types.NamespacedName]bool)}
}

func (r *celRegistry) add(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = true
}

func (r *celRegistry) remove(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, key)
}

func (r *celRegistry) count() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.cache))
}

func (r *reporter) observeCelCTM(_ context.Context, o metric.Int64Observer) error {
	o.Observe(r.celRegistry.count())
	return nil
}

func (r *reporter) ReportCelCT(templateName types.NamespacedName) {
	r.celRegistry.add(templateName)
}

func (r *reporter) DeleteCelCT(templateName types.NamespacedName) {
	r.celRegistry.remove(templateName)
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

func (r *reporter) reportCtMetric(status metrics.Status, count int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctReport == nil {
		r.ctReport = make(map[metrics.Status]int64)
	}
	r.ctReport[status] = count
	return nil
}

func (r *ctRegistry) remove(key types.NamespacedName) {
	if _, ok := r.cache[key]; !ok {
		return
	}
	delete(r.cache, key)
	r.dirty = true
}

func (r *ctRegistry) report(_ context.Context, mReporter *reporter) {
	if !r.dirty {
		return
	}
	totals := make(map[metrics.Status]int64)
	for _, status := range r.cache {
		totals[status]++
	}
	hadErr := false
	for _, status := range metrics.AllStatuses {
		if err := mReporter.reportCtMetric(status, totals[status]); err != nil {
			logger.Error(err, "failed to report total constraint templates")
			hadErr = true
		}
	}
	if !hadErr {
		r.dirty = false
	}
}

func (r *reporter) observeCTM(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for status, count := range r.ctReport {
		o.Observe(count, metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}

func (r *reporter) observeVAP(_ context.Context, observer metric.Int64Observer) error {
	totals := r.vapRegistry.ComputeTotals()
	for _, status := range metrics.AllVAPStatuses {
		observer.Observe(totals[status], metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}

func (r *reporter) ReportVAPStatus(templateName types.NamespacedName, status metrics.VAPStatus) {
	r.vapRegistry.Add(templateName, status)
}

func (r *reporter) DeleteVAPStatus(templateName types.NamespacedName) {
	r.vapRegistry.Remove(templateName)
}
