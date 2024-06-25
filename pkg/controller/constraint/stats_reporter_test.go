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
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r, err = newStatsReporter()
	assert.NoError(t, err)
	meter := mp.Meter("test")

	// Ensure the pipeline has a callback setup
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
