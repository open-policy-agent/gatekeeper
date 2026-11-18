package export

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestConnectionStateMetric(t *testing.T) {
	rdr := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr)).Meter("test")
	var state atomic.Int32
	require.NoError(t, registerConnectionStateMetric(meter, &state))

	for current := stateUnknown; current <= stateError; current++ {
		state.Store(int32(current))
		rm := metricdata.ResourceMetrics{}
		require.NoError(t, rdr.Collect(context.Background(), &rm))
		require.Len(t, rm.ScopeMetrics, 1)
		require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
		gauge, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Gauge[int64])
		require.True(t, ok)

		values := map[string]int64{}
		for _, point := range gauge.DataPoints {
			label, _ := point.Attributes.Value(connectionStateKey)
			values[label.AsString()] = point.Value
		}
		want := map[string]int64{"unknown": 0, "not_configured": 0, "ready": 0, "error": 0}
		want[connectionStateLabels[current]] = 1
		require.Equal(t, want, values)
	}
}
