package watch

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	gvkCountMetricName       = "watch_manager_watched_gvk"
	gvkIntentCountMetricName = "watch_manager_intended_watch_gvk"
)

var (
	meter           metric.Meter
	gvkCountM       metric.Int64ObservableGauge
	gvkIntentCountM metric.Int64ObservableGauge
)

func init() {
	var err error
	meterProvider := otel.GetMeterProvider()
	meter = meterProvider.Meter("gatekeeper")
	gvkCountM, err = meter.Int64ObservableGauge(
		gvkCountMetricName,
		metric.WithDescription("The total number of Group/Version/Kinds currently watched by the watch manager"),
	)
	if err != nil {
		panic(err)
	}
	gvkIntentCountM, err = meter.Int64ObservableGauge(
		gvkIntentCountMetricName,
		metric.WithDescription("The total number of Group/Version/Kinds that the watch manager has instructions to watch. This could differ from the actual count due to resources being pending, non-existent, or a failure of the watch manager to restart"))
	if err != nil {
		panic(err)
	}
}

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

func (r *reporter) registerCallback() error {
	_, err1 := meter.RegisterCallback(r.observeGvkIntentCount, gvkIntentCountM)
	_, err2 := meter.RegisterCallback(r.observeGvkCount, gvkCountM)

	return errors.Join(err1, err2)
}

// newStatsReporter creates a reporter for watch metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{}
	return r, r.registerCallback()
}

type reporter struct {
	mu          sync.RWMutex
	gvkCount    int64
	intentCount int64
}

func (r *reporter) observeGvkCount(ctx context.Context, observer metric.Observer) error {
	log.Info("reporting gvk count")
	observer.ObserveInt64(gvkCountM, r.gvkCount)
	return nil
}

// count returns total gvk count across all registrars.
func (r *reporter) observeGvkIntentCount(_ context.Context, observer metric.Observer) error {
	observer.ObserveInt64(gvkIntentCountM, r.intentCount)
	return nil
}
