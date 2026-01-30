package audit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	r, err = newStatsReporter(false) // disable constraint labels for backward compatibility tests
	assert.NoError(t, err)
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(violationsMetricName, metric.WithInt64Callback(r.observeTotalViolations))
	assert.NoError(t, err)
	auditDurationM, err = meter.Float64Histogram(auditDurationMetricName)
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunStartTimeMetricName, metric.WithFloat64Callback(r.observeRunStart))
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunEndTimeMetricName, metric.WithFloat64Callback(r.observeRunEnd))
	assert.NoError(t, err)

	return rdr, r
}

func initializeTestInstrumentsWithConstraintLabels(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	r, err = newStatsReporter(true) // enable constraint labels for new tests
	assert.NoError(t, err)
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	_, err = meter.Int64ObservableGauge(violationsMetricName, metric.WithInt64Callback(r.observeTotalViolations))
	assert.NoError(t, err)
	_, err = meter.Int64ObservableGauge(violationsPerConstraintMetricName, metric.WithInt64Callback(r.observeTotalViolationsWithConstraint))
	assert.NoError(t, err)
	auditDurationM, err = meter.Float64Histogram(auditDurationMetricName)
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunStartTimeMetricName, metric.WithFloat64Callback(r.observeRunStart))
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunEndTimeMetricName, metric.WithFloat64Callback(r.observeRunEnd))
	assert.NoError(t, err)

	return rdr, r
}

func TestReporter_observeTotalViolations(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
	}{
		{
			name:        "reporting total violations with attributes",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: violationsMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Deny))), Value: 1},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Dryrun))), Value: 2},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Warn))), Value: 3},
						{Attributes: attribute.NewSet(attribute.String(enforcementActionKey, string(util.Unrecognized))), Value: 4},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalViolationsPerEnforcementAction := map[util.EnforcementAction]int64{
				util.Deny:         1,
				util.Dryrun:       2,
				util.Warn:         3,
				util.Unrecognized: 4,
			}
			rdr, r := initializeTestInstruments(t)

			for k, v := range totalViolationsPerEnforcementAction {
				assert.NoError(t, r.reportTotalViolations(k, v))
			}

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReporter_reportLatency(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           *reporter
		duration    time.Duration
	}{
		{
			name:        "reporting audit latency",
			ctx:         context.Background(),
			expectedErr: nil,
			duration:    7000000000,
			want: metricdata.Metrics{
				Name: auditDurationMetricName,
				Data: metricdata.Histogram[float64]{
					Temporality: metricdata.CumulativeTemporality,
					DataPoints: []metricdata.HistogramDataPoint[float64]{
						{
							Attributes:   attribute.Set{},
							Count:        1,
							Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
							BucketCounts: []uint64{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
							Min:          metricdata.NewExtrema[float64](7.),
							Max:          metricdata.NewExtrema[float64](7.),
							Sum:          7,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			assert.NoError(t, r.reportLatency(tt.duration))

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))
			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReporter_observeRunStart(t *testing.T) {
	startTime := time.Now()
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		r           *reporter
	}{
		{
			name:        "reporting audit start time",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: lastRunStartTimeMetricName,
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{
						{Value: float64(startTime.Unix())},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			assert.NoError(t, r.reportRunStart(startTime))

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReporter_observeRunEnd(t *testing.T) {
	endTime := time.Now()
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
	}{
		{
			name:        "reporting audit end time",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: lastRunEndTimeMetricName,
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{
						{Value: float64(endTime.Unix())},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			assert.NoError(t, r.reportRunEnd(endTime))

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))
			fmt.Println(rm.ScopeMetrics[0])
			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReporter_observeTotalViolationsWithConstraint(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
	}{
		{
			name:        "reporting total violations with constraint labels",
			ctx:         context.Background(),
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraintViolations := map[util.KindVersionName]int64{
				{Kind: "TestKind", Version: "v1", Name: "test-constraint-1"}: 5,
				{Kind: "TestKind", Version: "v1", Name: "test-constraint-2"}: 3,
			}
			constraintEnforcementActions := map[util.KindVersionName]util.EnforcementAction{
				{Kind: "TestKind", Version: "v1", Name: "test-constraint-1"}: util.Deny,
				{Kind: "TestKind", Version: "v1", Name: "test-constraint-2"}: util.Warn,
			}

			rdr, r := initializeTestInstrumentsWithConstraintLabels(t)
			
			// Report both aggregated and per-constraint violations
			assert.NoError(t, r.reportTotalViolations(util.Deny, 5))
			assert.NoError(t, r.reportTotalViolations(util.Warn, 3))
			assert.NoError(t, r.reportTotalViolationsPerConstraint(constraintViolations, constraintEnforcementActions))

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			// Verify we have metrics
			assert.NotEmpty(t, rm.ScopeMetrics)
			assert.NotEmpty(t, rm.ScopeMetrics[0].Metrics)
			
			// Find the violations_per_constraint metric
			var perConstraintMetric *metricdata.Metrics
			var aggregatedMetric *metricdata.Metrics
			for i := range rm.ScopeMetrics[0].Metrics {
				if rm.ScopeMetrics[0].Metrics[i].Name == violationsPerConstraintMetricName {
					perConstraintMetric = &rm.ScopeMetrics[0].Metrics[i]
				}
				if rm.ScopeMetrics[0].Metrics[i].Name == violationsMetricName {
					aggregatedMetric = &rm.ScopeMetrics[0].Metrics[i]
				}
			}
			
			// Verify both metrics exist
			assert.NotNil(t, aggregatedMetric, "violations metric should be present")
			assert.NotNil(t, perConstraintMetric, "violations_per_constraint metric should be present")
			
			// Verify per-constraint metric has constraint labels
			gaugeData, ok := perConstraintMetric.Data.(metricdata.Gauge[int64])
			assert.True(t, ok, "Expected Gauge data type")
			assert.Len(t, gaugeData.DataPoints, 2)
			
			// Verify each data point has both enforcement_action and constraint labels
			for _, dp := range gaugeData.DataPoints {
				hasEnforcementAction := false
				hasConstraint := false
				for _, attr := range dp.Attributes.ToSlice() {
					if attr.Key == enforcementActionKey {
						hasEnforcementAction = true
					}
					if attr.Key == constraintKey {
						hasConstraint = true
					}
				}
				assert.True(t, hasEnforcementAction, "Data point missing enforcement_action label")
				assert.True(t, hasConstraint, "Data point missing constraint label")
			}
			
			// Verify aggregated metric has only enforcement_action labels
			aggGaugeData, ok := aggregatedMetric.Data.(metricdata.Gauge[int64])
			assert.True(t, ok, "Expected Gauge data type for aggregated metric")
			for _, dp := range aggGaugeData.DataPoints {
				hasConstraint := false
				for _, attr := range dp.Attributes.ToSlice() {
					if attr.Key == constraintKey {
						hasConstraint = true
					}
				}
				assert.False(t, hasConstraint, "Aggregated metric should not have constraint label")
			}
		})
	}
}
