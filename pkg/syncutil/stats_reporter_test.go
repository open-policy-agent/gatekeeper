package syncutil

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *Reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter = mp.Meter("test")

	r, err = NewStatsReporter()
	assert.NoError(t, err)
	_, err = meter.Int64ObservableGauge(syncMetricName, metric.WithInt64Callback(r.observeSync))
	assert.NoError(t, err)
	syncDurationM, err = meter.Float64Histogram(syncDurationMetricName)
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunTimeMetricName, metric.WithFloat64Callback(r.observeLastSync))
	assert.NoError(t, err)

	return rdr, r
}

func TestReportSync(t *testing.T) {
	wantTags := Tags{
		Kind:   "Pod",
		Status: metrics.ActiveStatus,
	}
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		c           *MetricsCache
	}{
		{
			name:        "reporting sync",
			ctx:         context.Background(),
			expectedErr: nil,
			c: &MetricsCache{
				Cache: map[string]Tags{
					"Pod": wantTags,
				},
				KnownKinds: map[string]bool{
					"Pod": true,
				},
			},
			want: metricdata.Metrics{
				Name: syncMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(kindKey, wantTags.Kind), attribute.String(statusKey, string(wantTags.Status))), Value: 1},
						{Attributes: attribute.NewSet(attribute.String(kindKey, wantTags.Kind), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			totals := make(map[Tags]int)
			for _, v := range tt.c.Cache {
				totals[v]++
			}

			for kind := range tt.c.KnownKinds {
				for _, status := range metrics.AllStatuses {
					if err := r.ReportSync(
						Tags{
							Kind:   kind,
							Status: status,
						},
						int64(totals[Tags{
							Kind:   kind,
							Status: status,
						}])); err != nil {
						log.Error(err, "failed to report sync")
					}
				}
			}

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))
			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReportSyncLatency(t *testing.T) {
	const minLatency = 100 * time.Second
	const maxLatency = 500 * time.Second
	const wantLatencyCount uint64 = 2
	const wantLatencyMin float64 = 100
	const wantLatencyMax float64 = 500

	want := metricdata.Metrics{
		Name: syncDurationMetricName,
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.Set{},
					Count:        wantLatencyCount,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](wantLatencyMin),
					Max:          metricdata.NewExtrema[float64](wantLatencyMax),
					Sum:          600,
				},
			},
		},
	}
	rdr, r := initializeTestInstruments(t)

	assert.NoError(t, r.ReportSyncDuration(minLatency))

	assert.NoError(t, r.ReportSyncDuration(maxLatency))

	rm := &metricdata.ResourceMetrics{}
	assert.Equal(t, nil, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
}

func TestLastRunSync(t *testing.T) {
	const wantTime float64 = 11

	fakeNow := func() float64 {
		return wantTime
	}

	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
	}{
		{
			name:        "reporting last sync run",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: lastRunTimeMetricName,
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{
						{Value: wantTime},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			r.now = fakeNow
			assert.NoError(t, r.ReportLastSync())

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}
