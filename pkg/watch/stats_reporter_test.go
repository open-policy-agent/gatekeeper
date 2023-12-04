package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
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

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(fnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	// Ensure the pipeline has a callback setup
	gvkIntentCountM, err = meter.Int64ObservableGauge(gvkIntentCountMetricName)
	assert.NoError(t, err)
	gvkCountM, err = meter.Int64ObservableGauge(gvkCountMetricName)
	assert.NoError(t, err)
	r, err = newStatsReporter()
	assert.NoError(t, err)
	return rdr, r
}

func TestReporter_reportGvkCount(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		wm          *Manager
	}{
		{
			name:        "reporting total violations with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			wm:          &Manager{watchedKinds: make(vitalsByGVK)},
			want: metricdata.Metrics{
				Name: gvkCountMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Value: 0},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			assert.NoError(t, r.reportGvkCount(0))
			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestRecordKeeper_reportGvkIntentCount(t *testing.T) {
	// var err error
	want := metricdata.Metrics{
		Name: gvkIntentCountMetricName,
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{Value: 0},
			},
		},
	}
	rdr, r := initializeTestInstruments(t)
	assert.NoError(t, r.reportGvkIntentCount(0))
	rm := &metricdata.ResourceMetrics{}
	assert.Equal(t, nil, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
}
