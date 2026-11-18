package export

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	connectionStateMetricName = "export_connection_state"
	connectionStateKey        = "state"
)

type connectionState int32

const (
	stateUnknown connectionState = iota
	stateNotConfigured
	stateReady
	stateError
)

var connectionStateLabels = [...]string{
	stateUnknown:       "unknown",
	stateNotConfigured: "not_configured",
	stateReady:         "ready",
	stateError:         "error",
}

// registerConnectionStateMetric reports 1 for the current state and 0 for the rest.
func registerConnectionStateMetric(meter metric.Meter, state *atomic.Int32) error {
	_, err := meter.Int64ObservableGauge(
		connectionStateMetricName,
		metric.WithDescription("State of the export connection configured for this pod, by state"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			current := connectionState(state.Load())
			for s := stateUnknown; s <= stateError; s++ {
				var v int64
				if s == current {
					v = 1
				}
				o.Observe(v, metric.WithAttributes(attribute.String(connectionStateKey, connectionStateLabels[s])))
			}
			return nil
		}),
	)
	return err
}
