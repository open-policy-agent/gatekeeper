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

func (r *reporter) observeConstraints(_ context.Context, observer metric.Int64Observer) error {
	r.mux.RLock()
	defer r.mux.RUnlock()
	for t, v := range r.constraintsReport {
		observer.Observe(v, metric.WithAttributes(attribute.String(enforcementActionKey, string(t.enforcementAction)), attribute.String(statusKey, string(t.status))))
	}
	return nil
}

func (r *reporter) reportConstraints(_ context.Context, t tags, v int64) error {
	r.mux.Lock()
	defer r.mux.Unlock()
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
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")
	_, err = meter.Int64ObservableGauge(
		constraintsMetricName,
		metric.WithDescription("Current number of known constraints"), metric.WithInt64Callback(r.observeConstraints))
	if err != nil {
		return nil, err
	}
	return r, nil
}

type reporter struct {
	mux               sync.RWMutex
	constraintsReport map[tags]int64
}
