package watch

import (
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

func (r *recordKeeper) registerGvkIntentCountMCallback() error {
	if _, err := meter.RegisterCallback(r.Count, gvkIntentCountM); err != nil {
		return err
	}
	return nil
}

func (r *reporter) registerGvkCountMCallBack(wm *Manager) error {
	if _, err := meter.RegisterCallback(wm.reportGvkCount, gvkCountM); err != nil {
		return err
	}
	return nil
}

// newStatsReporter creates a reporter for watch metrics.
func newStatsReporter() (*reporter, error) {
	return &reporter{}, nil
}

type reporter struct{}
