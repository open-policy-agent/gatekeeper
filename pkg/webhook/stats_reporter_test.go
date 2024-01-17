package webhook

import (
	"context"
	"testing"
	"time"

	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

const (
	minValidationDuration = 1 * time.Second
	maxValidationDuration = 5 * time.Second

	wantMinValidationSeconds float64 = 1
	wantMaxValidationSeconds float64 = 5

	wantCount     uint64 = 2
	wantRowLength int    = 1

	dryRun string = "false"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r StatsReporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r, err = newStatsReporter()
	assert.NoError(t, err)
	meter := mp.Meter("test")

	// Ensure the pipeline has a callback setup
	validationResponseTimeInSecM, err = meter.Float64Histogram(validationRequestDurationMetricName)
	assert.NoError(t, err)

	validationRequestCountM, err = meter.Int64Counter(validationRequestCountMetricName)
	assert.NoError(t, err)

	// Ensure the pipeline has a callback setup
	mutationResponseTimeInSecM, err = meter.Float64Histogram(mutationRequestDurationMetricName)
	assert.NoError(t, err)

	mutationRequestCountM, err = meter.Int64Counter(mutationRequestCountMetricName)
	assert.NoError(t, err)

	return rdr, r
}

func TestValidationReportRequest(t *testing.T) {
	ctx := context.Background()

	want1 := metricdata.Metrics{
		Name: validationRequestDurationMetricName,
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.NewSet(attribute.String(admissionStatusKey, string(successResponse))),
					Count:        wantCount,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](wantMinValidationSeconds),
					Max:          metricdata.NewExtrema[float64](wantMaxValidationSeconds),
					Sum:          6,
				},
			},
		},
	}
	want2 := metricdata.Metrics{
		Name: validationRequestCountMetricName,
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(admissionDryRunKey, dryRun), attribute.String(admissionStatusKey, string(successResponse))), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	rdr, r := initializeTestInstruments(t)

	assert.NoError(t, r.ReportValidationRequest(ctx, successResponse, dryRun, minValidationDuration))
	assert.NoError(t, r.ReportValidationRequest(ctx, successResponse, dryRun, maxValidationDuration))

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	metricdatatest.AssertEqual(t, want1, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
	metricdatatest.AssertEqual(t, want2, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
}

func TestMutationReportRequest(t *testing.T) {
	ctx := context.Background()

	want1 := metricdata.Metrics{
		Name: mutationRequestDurationMetricName,
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.NewSet(attribute.String(mutationStatusKey, string(successResponse))),
					Count:        wantCount,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](wantMinValidationSeconds),
					Max:          metricdata.NewExtrema[float64](wantMaxValidationSeconds),
					Sum:          6,
				},
			},
		},
	}
	want2 := metricdata.Metrics{
		Name: mutationRequestCountMetricName,
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(mutationStatusKey, string(successResponse))), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	rdr, r := initializeTestInstruments(t)

	assert.NoError(t, r.ReportMutationRequest(ctx, successResponse, minValidationDuration))
	assert.NoError(t, r.ReportMutationRequest(ctx, successResponse, maxValidationDuration))

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	metricdatatest.AssertEqual(t, want1, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
	metricdatatest.AssertEqual(t, want2, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
}
