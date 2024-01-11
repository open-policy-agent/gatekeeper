package webhook

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	validationRequestCountMetricName    = "validation_request_count"
	validationRequestDurationMetricName = "validation_request_duration_seconds"

	mutationRequestCountMetricName    = "mutation_request_count"
	mutationRequestDurationMetricName = "mutation_request_duration_seconds"

	admissionStatusKey = "admission_status"
	admissionDryRunKey = "admission_dryrun"
	mutationStatusKey  = "mutation_status"
)

var (
	validationResponseTimeInSecM metric.Float64Histogram
	mutationResponseTimeInSecM   metric.Float64Histogram
	mutationRequestCountM        metric.Int64Counter
	validationRequestCountM      metric.Int64Counter
	meter                        metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")

	validationResponseTimeInSecM, err = meter.Float64Histogram(
		validationRequestDurationMetricName,
		metric.WithDescription("The response time in seconds"),
		metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}

	validationRequestCountM, err = meter.Int64Counter(
		validationRequestCountMetricName,
		metric.WithDescription("The number of requests that are routed to validation webhook"))
	if err != nil {
		panic(err)
	}
	mutationResponseTimeInSecM, err = meter.Float64Histogram(
		mutationRequestDurationMetricName,
		metric.WithDescription("The response time in seconds"),
		metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
	mutationRequestCountM, err = meter.Int64Counter(
		mutationRequestCountMetricName,
		metric.WithDescription("The number of requests that are routed to mutation webhook"))
	if err != nil {
		panic(err)
	}

	view.Register(
		sdkmetric.NewView(
			sdkmetric.Instrument{Name: validationRequestDurationMetricName},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 1.5, 2, 2.5, 3},
				},
			},
		),
		sdkmetric.NewView(
			sdkmetric.Instrument{Name: mutationRequestDurationMetricName},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: []float64{0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 1.5, 2, 2.5, 3},
				},
			},
		),
	)
}

// StatsReporter reports webhook metrics.
type StatsReporter interface {
	ReportValidationRequest(ctx context.Context, response requestResponse, isDryRun string, d time.Duration) error
	ReportMutationRequest(ctx context.Context, response requestResponse, d time.Duration) error
}

// reporter implements StatsReporter interface.
type reporter struct{}

// newStatsReporter creaters a reporter for webhook metrics.
func newStatsReporter() (StatsReporter, error) {
	return &reporter{}, nil
}

func (r *reporter) ReportValidationRequest(ctx context.Context, response requestResponse, isDryRun string, d time.Duration) error {
	validationResponseTimeInSecM.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String(admissionStatusKey, string(response))))
	validationRequestCountM.Add(ctx, 1, metric.WithAttributes(attribute.String(admissionDryRunKey, isDryRun), attribute.String(admissionStatusKey, string(response))))
	return nil
}

func (r *reporter) ReportMutationRequest(ctx context.Context, response requestResponse, d time.Duration) error {
	mutationResponseTimeInSecM.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String(mutationStatusKey, string(response))))
	mutationRequestCountM.Add(ctx, 1, metric.WithAttributes(attribute.String(mutationStatusKey, string(response))))
	return nil
}
