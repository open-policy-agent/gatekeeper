package audit

import (
	"context"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	violationsMetricName       = "violations"
	auditDurationMetricName    = "audit_duration_seconds"
	lastRunStartTimeMetricName = "audit_last_run_time"
	lastRunEndTimeMetricName   = "audit_last_run_end_time"
	enforcementActionKey       = "enforcement_action"
)

var auditDurationM metric.Float64Histogram

func (r *reporter) observeTotalViolations(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for k, v := range r.totalViolationsPerEnforcementAction {
		o.Observe(v, metric.WithAttributes(attribute.String(enforcementActionKey, string(k))))
	}
	return nil
}

func (r *reporter) reportTotalViolations(enforcementAction util.EnforcementAction, v int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totalViolationsPerEnforcementAction == nil {
		r.totalViolationsPerEnforcementAction = make(map[util.EnforcementAction]int64)
	}
	r.totalViolationsPerEnforcementAction[enforcementAction] = v
	return nil
}

func (r *reporter) reportRunStart(t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startTime = t
	return nil
}

func (r *reporter) reportLatency(d time.Duration) error {
	auditDurationM.Record(context.Background(), d.Seconds())
	return nil
}

func (r *reporter) reportRunEnd(t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endTime = t
	return nil
}

func (r *reporter) observeRunStart(_ context.Context, o metric.Float64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o.Observe(float64(r.startTime.Unix()))
	return nil
}

func (r *reporter) observeRunEnd(_ context.Context, o metric.Float64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o.Observe(float64(r.endTime.Unix()))
	return nil
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")

	_, err = meter.Int64ObservableGauge(
		violationsMetricName,
		metric.WithDescription("Total number of audited violations"),
		metric.WithInt64Callback(r.observeTotalViolations),
	)

	if err != nil {
		return nil, err
	}

	auditDurationM, err = meter.Float64Histogram(
		auditDurationMetricName,
		metric.WithDescription("Latency of audit operation in seconds"))
	if err != nil {
		return nil, err
	}

	_, err = meter.Float64ObservableGauge(
		lastRunStartTimeMetricName,
		metric.WithDescription("Timestamp of last audit run starting time"),
		metric.WithFloat64Callback(r.observeRunStart),
	)
	if err != nil {
		return nil, err
	}

	_, err = meter.Float64ObservableGauge(
		lastRunEndTimeMetricName,
		metric.WithDescription("Timestamp of last audit run ending time"),
		metric.WithFloat64Callback(r.observeRunEnd),
	)
	if err != nil {
		return nil, err
	}

	view.Register(sdkmetric.NewView(
		sdkmetric.Instrument{Name: auditDurationMetricName},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{1 * 60, 3 * 60, 5 * 60, 10 * 60, 15 * 60, 20 * 60, 40 * 60, 80 * 60, 160 * 60, 320 * 60},
			},
		},
	))
	return r, nil
}

type reporter struct {
	mu                                  sync.RWMutex
	endTime                             time.Time
	startTime                           time.Time
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64
}
