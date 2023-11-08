package constrainttemplate

import (
	"context"
	"testing"
	"time"

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

func TestReportIngestion(t *testing.T) {
	var err error
	const (
		minIngestDuration = 1 * time.Second
		maxIngestDuration = 5 * time.Second
	)

	want1 := metricdata.Metrics{
		Name: "ingestDurationM",
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.NewSet(attribute.String(statusKey, string(metrics.ActiveStatus))),
					Count:        2,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](1.),
					Max:          metricdata.NewExtrema[float64](5.),
					Sum:          6,
				},
			},
		},
	}
	want2 := metricdata.Metrics{
		Name: "ingestCountM",
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	rdr := sdkmetric.NewPeriodicReader(new(fnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	// Ensure the pipeline has a callback setup
	ingestDurationM, err = meter.Float64Histogram("ingestDurationM")
	assert.NoError(t, err)

	ingestCountM, err = meter.Int64Counter("ingestCountM")
	assert.NoError(t, err)
	r := newStatsReporter()

	ctx := context.Background()

	err = r.reportIngestDuration(ctx, metrics.ActiveStatus, minIngestDuration)
	assert.NoError(t, err)

	err = r.reportIngestDuration(ctx, metrics.ActiveStatus, maxIngestDuration)
	assert.NoError(t, err)

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	metricdatatest.AssertEqual(t, want1, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
	metricdatatest.AssertEqual(t, want2, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
}

func TestObserveCTM(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		c           *ctRegistry
	}{
		{
			name:        "reporting total expansion templates with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			c: &ctRegistry{
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
			ctM, err = meter.Int64ObservableGauge("test")
			assert.NoError(t, err)
			_, err = meter.RegisterCallback(tt.c.observeCTM, ctM)
			assert.NoError(t, err)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
