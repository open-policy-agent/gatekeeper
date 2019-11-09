package webhook

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	requestCountName     = "request_count"
	requestLatenciesName = "request_latencies"
)

var (
	responseTimeInSecM = stats.Float64(
		requestLatenciesName,
		"The response time in seconds",
		stats.UnitSeconds)

	admissionStatusKey = tag.MustNewKey("admission_status")
)

func init() {
	register()
}

// StatsReporter reports webhook metrics
type StatsReporter interface {
	ReportRequest(response string, d time.Duration) error
}

// reporter implements StatsReporter interface
type reporter struct {
	ctx context.Context
}

// NewStatsReporter creaters a reporter for webhook metrics
func NewStatsReporter() (StatsReporter, error) {
	ctx, err := tag.New(
		context.Background(),
	)
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

// Captures req count metric, recording the count and the duration
func (r *reporter) ReportRequest(response string, d time.Duration) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(admissionStatusKey, response),
	)
	if err != nil {
		return err
	}

	r.report(ctx, responseTimeInSecM.M(1))
	// Convert time.Duration in nanoseconds to seconds
	r.report(ctx, responseTimeInSecM.M(float64(d/time.Second)))
	return nil
}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	metrics.Record(ctx, m)
	return nil
}

func register() {
	if err := view.Register(
		&view.View{
			Name:        requestCountName,
			Description: "The number of requests that are routed to webhook",
			Measure:     responseTimeInSecM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{admissionStatusKey},
		},
		&view.View{
			Name:        requestLatenciesName,
			Description: responseTimeInSecM.Description(),
			Measure:     responseTimeInSecM,
			Aggregation: view.Distribution(1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000, 11000, 12000, 13000, 14000, 15000),
			TagKeys:     []tag.Key{admissionStatusKey},
		},
	); err != nil {
		panic(err)
	}
}
