package config

import (
	"context"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	cfgMetricName = "config"
	statusKey     = "status"
)

func (r *reporter) observeConfig(_ context.Context, observer metric.Int64Observer) error {
	r.mux.RLock()
	defer r.mux.RUnlock()
	for t, v := range r.configReport {
		observer.Observe(v, metric.WithAttributes(attribute.String(statusKey, string(t))))
	}
	return nil
}

func (r *reporter) reportConfig(_ context.Context, t metrics.Status, v int64) error {
	r.mux.Lock()
	defer r.mux.Unlock()
	if r.configReport == nil {
		r.configReport = make(map[metrics.Status]int64)
	}
	r.configReport[t] = v
	return nil
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")
	_, err = meter.Int64ObservableGauge(
		cfgMetricName,
		metric.WithDescription("Config Status"), metric.WithInt64Callback(r.observeConfig))
	if err != nil {
		return nil, err
	}
	return r, nil
}

type reporter struct {
	mux          sync.RWMutex
	configReport map[metrics.Status]int64
}
