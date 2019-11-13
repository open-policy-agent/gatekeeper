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
	requestCountName    = "request_count"
	requestDurationName = "request_duration_seconds"
)

var (
	responseTimeInSecM = stats.Float64(
		requestDurationName,
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

	r.report(ctx, responseTimeInSecM.M(d.Seconds()))
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
			Name:        requestDurationName,
			Description: responseTimeInSecM.Description(),
			Measure:     responseTimeInSecM,
			Aggregation: view.Distribution(0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.01, 0.02, 0.03, 0.04, 0.05),
			TagKeys:     []tag.Key{admissionStatusKey},
		},
	); err != nil {
		panic(err)
	}
}
