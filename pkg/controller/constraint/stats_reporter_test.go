package constraint

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
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
	r, err = newStatsReporter()
	assert.NoError(t, err)
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(constraintsMetricName, metric.WithInt64Callback(r.observeConstraints))
	assert.NoError(t, err)
	return rdr, r
}

func TestReportConstraints(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
	}{
		{
			name:        "reporting total constraint with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: constraintsMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Warn)), attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Deny)), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Dryrun)), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Warn)), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Deny)), attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Dryrun)), attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Scoped)), attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Scoped)), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Unrecognized)), attribute.String(statusKey, string(metrics.ActiveStatus))), Value: 0},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Unrecognized)), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			for _, enforcementAction := range util.KnownEnforcementActions {
				for _, status := range metrics.AllStatuses {
					assert.NoError(t, r.reportConstraints(tt.ctx, tags{
						enforcementAction: enforcementAction,
						status:            status,
					}, 0))
				}
			}

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
	r, err = newStatsReporter()
	assert.NoError(t, err)
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(vapbMetricName, metric.WithInt64Callback(r.observeVAPB))
	assert.NoError(t, err)

	return rdr, r
}

func TestVAPBMetrics(t *testing.T) {
	rdr, r := initializeVAPTestInstruments(t)
	ctx := context.Background()

	assert.NoError(t, r.ReportVAPBStatus(ctx, types.NamespacedName{Name: "vapb-1"}, VAPStatusActive))
	assert.NoError(t, r.ReportVAPBStatus(ctx, types.NamespacedName{Name: "vapb-2"}, VAPStatusActive))
	assert.NoError(t, r.ReportVAPBStatus(ctx, types.NamespacedName{Name: "vapb-3"}, VAPStatusActive))
	assert.NoError(t, r.ReportVAPBStatus(ctx, types.NamespacedName{Name: "vapb-4"}, VAPStatusError))

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var vapbMetric *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == vapbMetricName {
				vapbMetric = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, vapbMetric)

	gaugeData, ok := vapbMetric.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)

	statusCounts := make(map[string]int64)
	for _, dp := range gaugeData.DataPoints {
		statusAttr, _ := dp.Attributes.Value(attribute.Key(statusKey))
		statusCounts[statusAttr.AsString()] = dp.Value
	}

	assert.Equal(t, int64(3), statusCounts[string(VAPStatusActive)])
	assert.Equal(t, int64(1), statusCounts[string(VAPStatusError)])
}

func TestReportDeleteVAPBStatus(t *testing.T) {
	rdr, r := initializeVAPTestInstruments(t)
	ctx := context.Background()

	vapbName := types.NamespacedName{Name: "vapb-to-delete"}
	assert.NoError(t, r.ReportVAPBStatus(ctx, vapbName, VAPStatusActive))

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	var vapbMetricBefore *metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == vapbMetricName {
				vapbMetricBefore = &sm.Metrics[i]
				break
			}
		}
	}
	assert.NotNil(t, vapbMetricBefore)
	gaugeDataBefore, ok := vapbMetricBefore.Data.(metricdata.Gauge[int64])
	assert.True(t, ok)
	assert.Len(t, gaugeDataBefore.DataPoints, 2)
	statusCountsBefore := make(map[string]int64)
	for _, dp := range gaugeDataBefore.DataPoints {
		for _, attr := range dp.Attributes.ToSlice() {
			if attr.Key == statusKey {
				statusCountsBefore[attr.Value.AsString()] = dp.Value
			}
		}
	}
	assert.Equal(t, int64(1), statusCountsBefore[string(VAPStatusActive)])
	assert.Equal(t, int64(0), statusCountsBefore[string(VAPStatusError)])

	assert.NoError(t, r.DeleteVAPBStatus(ctx, vapbName))

	totals := r.vapbRegistry.computeTotals()
	assert.Equal(t, int64(0), totals[VAPStatusActive])
	assert.Equal(t, int64(0), totals[VAPStatusError])
}

func TestVAPBStatusUpdate(t *testing.T) {
	_, r := initializeVAPTestInstruments(t)
	ctx := context.Background()

	vapbName := types.NamespacedName{Name: "vapb-update-test"}

	assert.NoError(t, r.ReportVAPBStatus(ctx, vapbName, VAPStatusError))

	totals := r.vapbRegistry.computeTotals()
	assert.Equal(t, int64(1), totals[VAPStatusError])
	assert.Equal(t, int64(0), totals[VAPStatusActive])

	assert.NoError(t, r.ReportVAPBStatus(ctx, vapbName, VAPStatusActive))

	totals = r.vapbRegistry.computeTotals()
	assert.Equal(t, int64(0), totals[VAPStatusError])
	assert.Equal(t, int64(1), totals[VAPStatusActive])
}
