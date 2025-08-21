package externaldata

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"k8s.io/apimachinery/pkg/types"
)

func TestReporter_add(t *testing.T) {
	r := &reporter{
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

func TestReporter_remove(t *testing.T) {
	r := &reporter{
		cache: map[types.NamespacedName]metrics.Status{{Name: "test"}: metrics.ActiveStatus},
		dirty: true,
	}

	// Test removing an existing key
	r.remove(types.NamespacedName{Name: "test"})
	if _, exists := r.cache[types.NamespacedName{Name: "test"}]; exists {
		t.Error("Expected key to be removed from cache")
	}
	if r.dirty != true {
		t.Error("Expected dirty flag to be set to true")
	}

	// Test removing a non-existing key
	r.remove(types.NamespacedName{Name: "non-existing"})
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
		r           *reporter
		dirty       bool
	}{
		{
			name:        "reporting total expansion templates with attributes",
			ctx:         context.Background(),
			dirty:       false,
			expectedErr: nil,
			r: &reporter{
				dirty: true,
				cache: map[types.NamespacedName]metrics.Status{
					{Name: "test1"}: metrics.ActiveStatus,
					{Name: "test2"}: metrics.ErrorStatus,
					{Name: "test3"}: metrics.ActiveStatus,
				},
			},
			want: metricdata.Metrics{
				Name: providerMetricName,
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

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	r = newStatsReporter()
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(providerMetricName, metric.WithInt64Callback(r.observeProviderMetric))
	assert.NoError(t, err)

	// Also initialize the error counter metric that reportProviderError uses
	providerErrorCountM, err = meter.Int64Counter(providerErrorCountName)
	assert.NoError(t, err)

	return rdr, r
}

func TestReportProviderErrors(t *testing.T) {
	want := metricdata.Metrics{
		Name: providerErrorCountName,
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	ctx := context.Background()
	rdr, r := initializeTestInstruments(t)
	r.reportProviderError(ctx)
	r.reportProviderError(ctx)

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	metricdatatest.AssertEqual(t, want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
}
