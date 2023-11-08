package expansion

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"k8s.io/apimachinery/pkg/types"
)

type fnExporter struct {
	temporalityFunc sdkmetric.TemporalitySelector
	aggregationFunc sdkmetric.AggregationSelector
	exportFunc      func(context.Context, *metricdata.ResourceMetrics) error
	flushFunc       func(context.Context) error
	shutdownFunc    func(context.Context) error
}

func (e *fnExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	if e.temporalityFunc != nil {
		return e.temporalityFunc(k)
	}
	return sdkmetric.DefaultTemporalitySelector(k)
}

func (e *fnExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	if e.aggregationFunc != nil {
		return e.aggregationFunc(k)
	}
	return sdkmetric.DefaultAggregationSelector(k)
}

func (e *fnExporter) Export(ctx context.Context, m *metricdata.ResourceMetrics) error {
	if e.exportFunc != nil {
		return e.exportFunc(ctx, m)
	}
	return nil
}

func (e *fnExporter) ForceFlush(ctx context.Context) error {
	if e.flushFunc != nil {
		return e.flushFunc(ctx)
	}
	return nil
}

func (e *fnExporter) Shutdown(ctx context.Context) error {
	if e.shutdownFunc != nil {
		return e.shutdownFunc(ctx)
	}
	return nil
}

func TestEtRegistry_add(t *testing.T) {
	r := &etRegistry{
		cache: make(map[types.NamespacedName]metrics.Status),
	}

	// Add a new entry
	key := types.NamespacedName{Name: "test-name", Namespace: "test-namespace"}
	status := metrics.ActiveStatus
	r.add(key, status)

	// Check that the entry was added correctly
	if len(r.cache) != 1 {
		t.Errorf("Expected cache length 1, got %d", len(r.cache))
	}
	if _, ok := r.cache[key]; !ok {
		t.Errorf("Expected key %v to be in cache", key)
	}
	if r.cache[key] != status {
		t.Errorf("Expected status %v, got %v", status, r.cache[key])
	}

	// Add an existing entry with the same status
	r.add(key, status)

	// Check that the entry was not added again
	if len(r.cache) != 1 {
		t.Errorf("Expected cache length 1, got %d", len(r.cache))
	}
	if _, ok := r.cache[key]; !ok {
		t.Errorf("Expected key %v to be in cache", key)
	}
	if r.cache[key] != status {
		t.Errorf("Expected status %v, got %v", status, r.cache[key])
	}

	// Add an existing entry with a different status
	newStatus := metrics.ErrorStatus
	r.add(key, newStatus)

	// Check that the entry was updated with the new status
	if len(r.cache) != 1 {
		t.Errorf("Expected cache length 1, got %d", len(r.cache))
	}
	if _, ok := r.cache[key]; !ok {
		t.Errorf("Expected key %v to be in cache", key)
	}
	if r.cache[key] != newStatus {
		t.Errorf("Expected status %v, got %v", newStatus, r.cache[key])
	}
}

func TestEtRegistry_remove(t *testing.T) {
	r := &etRegistry{
		cache: map[types.NamespacedName]metrics.Status{{Name: "test", Namespace: "default"}: metrics.ActiveStatus},
		dirty: true,
	}

	// Test removing an existing key
	r.remove(types.NamespacedName{Name: "test", Namespace: "default"})
	if _, exists := r.cache[types.NamespacedName{Name: "test", Namespace: "default"}]; exists {
		t.Error("Expected key to be removed from cache")
	}
	if r.dirty != true {
		t.Error("Expected dirty flag to be set to true")
	}

	// Test removing a non-existing key
	r.remove(types.NamespacedName{Name: "non-existing", Namespace: "default"})
	if r.dirty != true {
		t.Error("Expected dirty flag to remain true")
	}
}

func TestReport(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           *etRegistry
	}{
		{
			name:        "reporting total expansion templates with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			r: &etRegistry{
				dirty: true,
				cache: map[types.NamespacedName]metrics.Status{
					{Name: "test1", Namespace: "default"}: metrics.ActiveStatus,
					{Name: "test2", Namespace: "default"}: metrics.ErrorStatus,
					{Name: "test3", Namespace: "default"}: metrics.ActiveStatus,
				},
			},
			want: metricdata.Metrics{
				Name: "test",
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 2},
						{Attributes: attribute.NewSet(attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 1},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			rdr := sdkmetric.NewPeriodicReader(new(fnExporter))
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
			meter := mp.Meter("test")

			// Ensure the pipeline has a callback setup
			etM, err = meter.Int64ObservableGauge("test")
			assert.NoError(t, err)
			_, err = meter.RegisterCallback(tt.r.observeETM, etM)
			assert.NoError(t, err)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
