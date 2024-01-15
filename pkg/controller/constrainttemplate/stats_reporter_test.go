package constrainttemplate

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"k8s.io/apimachinery/pkg/types"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	// Ensure the pipeline has a callback setup
	ingestDurationM, err = meter.Float64Histogram(ingestDuration)
	assert.NoError(t, err)

	ingestCountM, err = meter.Int64Counter(ingestCount)
	assert.NoError(t, err)

	ctM, err = meter.Int64ObservableGauge(ctMetricName)
	assert.NoError(t, err)
	r = newStatsReporter()
	return rdr, r
}

func TestReportIngestion(t *testing.T) {
	const (
		minIngestDuration = 1 * time.Second
		maxIngestDuration = 5 * time.Second
	)

	want1 := metricdata.Metrics{
		Name: ingestDuration,
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
		Name: ingestCount,
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	ctx := context.Background()
	rdr, r := initializeTestInstruments(t)
	assert.NoError(t, r.reportIngestDuration(ctx, metrics.ActiveStatus, minIngestDuration))

	assert.NoError(t, r.reportIngestDuration(ctx, metrics.ActiveStatus, maxIngestDuration))

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
				Name: ctMetricName,
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
			rdr, r := initializeTestInstruments(t)
			tt.c.report(tt.ctx, r)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
