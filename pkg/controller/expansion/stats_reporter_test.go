package expansion

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"k8s.io/apimachinery/pkg/types"
)

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

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *etRegistry) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	etM, err = meter.Int64ObservableGauge(etMetricName)
	assert.NoError(t, err)
	r = newRegistry()

	return rdr, r
}

func TestReport(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           *etRegistry
		dirty       bool
	}{
		{
			name:        "reporting total expansion templates with attributes",
			ctx:         context.Background(),
			dirty:       false,
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
				Name: etMetricName,
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
			rdr, reg := initializeTestInstruments(t)
			reg.dirty = tt.r.dirty
			reg.cache = tt.r.cache
			reg.report(tt.ctx)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
