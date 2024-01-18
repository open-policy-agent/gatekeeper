package watch

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	gvkCountMetricName       = "watch_manager_watched_gvk"
	gvkIntentCountMetricName = "watch_manager_intended_watch_gvk"
)

var r *reporter

func (r *reporter) reportGvkCount(count int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gvkCount = count
	return nil
}

func (r *reporter) reportGvkIntentCount(count int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intentCount = count
	return nil
}

// newStatsReporter creates a reporter for watch metrics.
func newStatsReporter() (*reporter, error) {
	if r == nil {
		var err error
		meterProvider := otel.GetMeterProvider()
		meter := meterProvider.Meter("gatekeeper")
		r = &reporter{}
		_, err = meter.Int64ObservableGauge(
			gvkCountMetricName,
			metric.WithDescription("The total number of Group/Version/Kinds currently watched by the watch manager"),
			metric.WithInt64Callback(r.observeGvkCount),
		)
		if err != nil {
			return nil, err
		}
		_, err = meter.Int64ObservableGauge(
			gvkIntentCountMetricName,
			metric.WithDescription("The total number of Group/Version/Kinds that the watch manager has instructions to watch. This could differ from the actual count due to resources being pending, non-existent, or a failure of the watch manager to restart"),
			metric.WithInt64Callback(r.observeGvkIntentCount),
		)
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

type reporter struct {
	mu          sync.RWMutex
	gvkCount    int64
	intentCount int64
}

func (r *reporter) observeGvkCount(_ context.Context, observer metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	observer.Observe(r.gvkCount)
	return nil
}

// count returns total gvk count across all registrars.
func (r *reporter) observeGvkIntentCount(_ context.Context, observer metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	observer.Observe(r.intentCount)
	return nil
}
