package mutation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
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

func TestReportIterationConvergence(t *testing.T) {
	const (
		successMax = 5
		successMin = 3
		failureMax = 8
		failureMin = failureMax
	)
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           StatsReporter
		max         int
		min         int
		status      SystemConvergenceStatus
	}{
		{
			name:        "recording successful iteration convergence",
			ctx:         context.Background(),
			expectedErr: nil,
			r:           NewStatsReporter(),
			max:         successMax,
			min:         successMin,
			status:      SystemConvergenceTrue,
			want: metricdata.Metrics{
				Name: "test",
				Data: metricdata.Histogram[int64]{
					Temporality: metricdata.CumulativeTemporality,
					DataPoints: []metricdata.HistogramDataPoint[int64]{
						{
							Attributes:   attribute.NewSet(attribute.String(systemConvergenceKey, string(SystemConvergenceTrue))),
							Count:        2,
							Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
							BucketCounts: []uint64{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
							Min:          metricdata.NewExtrema[int64](successMin),
							Max:          metricdata.NewExtrema[int64](successMax),
							Sum:          8,
						},
					},
				},
			},
		},
		{
			name:        "recording failed iteration convergence",
			ctx:         context.Background(),
			expectedErr: nil,
			r:           NewStatsReporter(),
			max:         failureMax,
			min:         failureMin,
			status:      SystemConvergenceFalse,
			want: metricdata.Metrics{
				Name: "test",
				Data: metricdata.Histogram[int64]{
					Temporality: metricdata.CumulativeTemporality,
					DataPoints: []metricdata.HistogramDataPoint[int64]{
						{
							Attributes:   attribute.NewSet(attribute.String(systemConvergenceKey, string(SystemConvergenceFalse))),
							Count:        2,
							Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
							BucketCounts: []uint64{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
							Min:          metricdata.NewExtrema[int64](failureMin),
							Max:          metricdata.NewExtrema[int64](failureMax),
							Sum:          16,
						},
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
			systemIterationsM, err = meter.Int64Histogram("test")
			assert.NoError(t, err)
			assert.NoError(t, tt.r.ReportIterationConvergence(tt.status, tt.max))
			assert.NoError(t, tt.r.ReportIterationConvergence(tt.status, tt.min))

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))
			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
