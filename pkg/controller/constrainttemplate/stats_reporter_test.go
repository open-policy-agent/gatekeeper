package constrainttemplate

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
	"k8s.io/apimachinery/pkg/types"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r = newStatsReporter()
	meter := mp.Meter("test")

	ingestDurationM, err = meter.Float64Histogram(ingestDuration)
	assert.NoError(t, err)

	ingestCountM, err = meter.Int64Counter(ingestCount)
	assert.NoError(t, err)

	_, err = meter.Int64ObservableGauge(ctMetricName, metric.WithInt64Callback(r.observeCTM))
	assert.NoError(t, err)
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

func initializeVAPTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r = newStatsReporter()
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(vapMetricName, metric.WithInt64Callback(r.observeVAP))
	assert.NoError(t, err)

	return rdr, r
}

func TestVAPMetrics(t *testing.T) {
	rdr, r := initializeVAPTestInstruments(t)
	ctx := context.Background()

	r.ReportVAPStatus(types.NamespacedName{Name: "template-1"}, metrics.VAPStatusActive)
	r.ReportVAPStatus(types.NamespacedName{Name: "template-2"}, metrics.VAPStatusActive)
	r.ReportVAPStatus(types.NamespacedName{Name: "template-3"}, metrics.VAPStatusError)

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var vapMetric *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == vapMetricName {
				vapMetric = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, vapMetric)

	gaugeData, ok := vapMetric.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)

	statusCounts := make(map[string]int64)
	for _, dp := range gaugeData.DataPoints {
		statusAttr, exists := dp.Attributes.Value(attribute.Key(statusKey))
		if exists {
			statusCounts[statusAttr.AsString()] = dp.Value
		}
	}

	assert.Equal(t, int64(2), statusCounts[string(metrics.VAPStatusActive)])
	assert.Equal(t, int64(1), statusCounts[string(metrics.VAPStatusError)])
}

func TestReportDeleteVAPStatus(t *testing.T) {
	rdr, r := initializeVAPTestInstruments(t)
	ctx := context.Background()

	templateName := types.NamespacedName{Name: "template-to-delete"}
	r.ReportVAPStatus(templateName, metrics.VAPStatusActive)

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var vapMetricBefore *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == vapMetricName {
				vapMetricBefore = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, vapMetricBefore)
	gaugeDataBefore, ok := vapMetricBefore.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)
	statusCountsBefore := make(map[string]int64)
	for _, dp := range gaugeDataBefore.DataPoints {
		statusAttr, exists := dp.Attributes.Value(attribute.Key(statusKey))
		if exists {
			statusCountsBefore[statusAttr.AsString()] = dp.Value
		}
	}
	assert.Equal(t, int64(1), statusCountsBefore[string(metrics.VAPStatusActive)])

	r.DeleteVAPStatus(templateName)

	rm = &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var vapMetric *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == vapMetricName {
				vapMetric = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, vapMetric)

	gaugeData, ok := vapMetric.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)

	for _, dp := range gaugeData.DataPoints {
		assert.Equal(t, int64(0), dp.Value)
	}
}

func TestVAPStatusUpdate(t *testing.T) {
	_, r := initializeVAPTestInstruments(t)

	templateName := types.NamespacedName{Name: "template-update-test"}

	r.ReportVAPStatus(templateName, metrics.VAPStatusError)

	totals := r.vapRegistry.ComputeTotals()
	assert.Equal(t, int64(1), totals[metrics.VAPStatusError])
	assert.Equal(t, int64(0), totals[metrics.VAPStatusActive])

	r.ReportVAPStatus(templateName, metrics.VAPStatusActive)

	totals = r.vapRegistry.ComputeTotals()
	assert.Equal(t, int64(0), totals[metrics.VAPStatusError])
	assert.Equal(t, int64(1), totals[metrics.VAPStatusActive])
}

func initializeCelTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r = newStatsReporter()
	meter := mp.Meter("test")

	_, err := meter.Int64ObservableGauge(celCTMetricName, metric.WithInt64Callback(r.observeCelCTM))
	assert.NoError(t, err)

	return rdr, r
}

func TestCelCTMetrics(t *testing.T) {
	rdr, r := initializeCelTestInstruments(t)
	ctx := context.Background()

	r.ReportCelCT(types.NamespacedName{Name: "template-1"})
	r.ReportCelCT(types.NamespacedName{Name: "template-2"})
	r.ReportCelCT(types.NamespacedName{Name: "template-3"})

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var celMetric *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == celCTMetricName {
				celMetric = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, celMetric)

	gaugeData, ok := celMetric.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)
	assert.Len(t, gaugeData.DataPoints, 1)
	assert.Equal(t, int64(3), gaugeData.DataPoints[0].Value)
}

func TestDeleteCelCT(t *testing.T) {
	_, r := initializeCelTestInstruments(t)

	r.ReportCelCT(types.NamespacedName{Name: "template-1"})
	r.ReportCelCT(types.NamespacedName{Name: "template-2"})

	assert.Equal(t, int64(2), r.celRegistry.count())

	r.DeleteCelCT(types.NamespacedName{Name: "template-1"})

	assert.Equal(t, int64(1), r.celRegistry.count())

	r.DeleteCelCT(types.NamespacedName{Name: "template-2"})

	assert.Equal(t, int64(0), r.celRegistry.count())
}
