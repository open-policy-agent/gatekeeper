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

func TestReporter_registerGvkCountMCallBack(t *testing.T) {
	r, err := newStatsReporter()
	if err != nil {
		t.Fatalf("newStatsReporter() error %v", err)
	}
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           *reporter
		wm          *Manager
	}{
		{
			name:        "reporting total violations with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			r:           r,
			wm:          &Manager{watchedKinds: make(vitalsByGVK)},
			want: metricdata.Metrics{
				Name: "test",
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
			rdr := sdkmetric.NewPeriodicReader(new(fnExporter))
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
			meter = mp.Meter("test")

			// Ensure the pipeline has a callback setup
			gvkCountM, err = meter.Int64ObservableGauge("test")
			assert.NoError(t, err)
			err = r.registerGvkCountMCallBack(tt.wm)
			assert.NoError(t, err)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestRecordKeeper_registerGvkIntentCountMCallback(t *testing.T) {
	var err error
	want := metricdata.Metrics{
		Name: "test",
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{Value: 0},
			},
		},
	}

	rdr := sdkmetric.NewPeriodicReader(new(fnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	// Ensure the pipeline has a callback setup
	gvkIntentCountM, err = meter.Int64ObservableGauge("test")
	assert.NoError(t, err)
	r, err := newRecordKeeper()
	if err != nil {
		t.Fatalf("newRecordKeeper() error %v", err)
	}

	// Register the callback
	err = r.registerGvkIntentCountMCallback()
	assert.NoError(t, err)

	rm := &metricdata.ResourceMetrics{}
	assert.Equal(t, nil, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
}

func InitializeTestInstruments(t *testing.T) {
	var err error
	rdr := sdkmetric.NewPeriodicReader(new(fnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	// Ensure the pipeline has a callback setup
	gvkIntentCountM, err = meter.Int64ObservableGauge("test")
	assert.NoError(t, err)
	gvkCountM, err = meter.Int64ObservableGauge("test")
	assert.NoError(t, err)
}
