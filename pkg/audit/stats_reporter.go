package audit

import (
	"context"
	"errors"
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

var (
	violationsM       metric.Int64ObservableGauge
	auditDurationM    metric.Float64Histogram
	lastRunStartTimeM metric.Float64ObservableGauge
	lastRunEndTimeM   metric.Float64ObservableGauge
	meter             metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")

	violationsM, err = meter.Int64ObservableGauge(
		violationsMetricName,
		metric.WithDescription("Total number of audited violations"),
	)

	if err != nil {
		panic(err)
	}

	auditDurationM, err = meter.Float64Histogram(
		auditDurationMetricName,
		metric.WithDescription("Latency of audit operation in seconds"))
	if err != nil {
		panic(err)
	}

	lastRunStartTimeM, err = meter.Float64ObservableGauge(
		lastRunStartTimeMetricName,
		metric.WithDescription("Timestamp of last audit run starting time"),
	)
	if err != nil {
		panic(err)
	}

	lastRunEndTimeM, err = meter.Float64ObservableGauge(
		lastRunEndTimeMetricName,
		metric.WithDescription("Timestamp of last audit run ending time"),
	)
	if err != nil {
		panic(err)
	}

	view.Register(sdkmetric.NewView(
		sdkmetric.Instrument{Name: auditDurationMetricName},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{1 * 60, 3 * 60, 5 * 60, 10 * 60, 15 * 60, 20 * 60, 40 * 60, 80 * 60, 160 * 60, 320 * 60},
			},
		},
	))
}

func (r *reporter) registerCallback() error {
	_, err1 := meter.RegisterCallback(r.observeTotalViolations, violationsM)
	_, err2 := meter.RegisterCallback(r.observeRunEnd, lastRunEndTimeM)
	_, err3 := meter.RegisterCallback(r.observeRunStart, lastRunStartTimeM)
	return errors.Join(err1, err2, err3)
}

func (r *reporter) observeTotalViolations(_ context.Context, o metric.Observer) error {
	for k, v := range r.totalViolationsPerEnforcementAction {
		o.ObserveInt64(violationsM, v, metric.WithAttributes(attribute.String(enforcementActionKey, string(k))))
	}
	return nil
}

func (r *reporter) reportLatency(d time.Duration) error {
	auditDurationM.Record(context.Background(), d.Seconds())
	return nil
}

func (r *reporter) observeRunStart(_ context.Context, o metric.Observer) error {
	o.ObserveFloat64(lastRunStartTimeM, float64(r.startTime.Unix()))
	return nil
}

func (r *reporter) observeRunEnd(_ context.Context, o metric.Observer) error {
	o.ObserveFloat64(lastRunEndTimeM, float64(r.endTime.Unix()))
	return nil
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() *reporter {
	return &reporter{}
}

type reporter struct {
	endTime                             time.Time
	startTime                           time.Time
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64
}
