package constraint

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	constraintsMetricName = "constraints"
	enforcementActionKey  = "enforcement_action"
	statusKey             = "status"
)

var (
	constraintsM metric.Int64ObservableGauge
	meter        metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")
	constraintsM, err = meter.Int64ObservableGauge(
		constraintsMetricName,
		metric.WithDescription("Current number of known constraints"))
	if err != nil {
		panic(err)
	}
}

func (r *reporter) observeConstraints(ctx context.Context, observer metric.Observer) error {
	for t, v := range r.constraintsReport {
		observer.ObserveInt64(constraintsM, int64(v), metric.WithAttributes(attribute.String(enforcementActionKey, string(t.enforcementAction)), attribute.String(statusKey, string(t.status))))
	}
	return nil
}

func (r *reporter) reportConstraints(ctx context.Context, t tags, v int64) error {
	r.mux.RLock()
	defer r.mux.RUnlock()
	if r.constraintsReport == nil {
		r.constraintsReport = make(map[tags]int64)
	}
	r.constraintsReport[t] = v
	return nil
}

// StatsReporter reports audit metrics.
type StatsReporter interface {
	reportConstraints(ctx context.Context, t tags, v int64) error
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{}
	return r, r.registerCallback()
}

func (r *reporter) registerCallback() error {
	_, err := meter.RegisterCallback(r.observeConstraints, constraintsM)
	return err
}

type reporter struct {
	mux               sync.RWMutex
	constraintsReport map[tags]int64
}
