package audit

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	violationsMetricName    = "violations"
	auditDurationMetricName = "audit_duration_seconds"
)

var (
	violationsM    = stats.Int64(violationsMetricName, "Total number of violations per constraint", stats.UnitDimensionless)
	auditDurationM = stats.Float64(auditDurationMetricName, "Latency of audit operation in seconds", stats.UnitSeconds)

	enforcementActionKey = tag.MustNewKey("enforcement_action")
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

func register() error {
	views := []*view.View{
		{
			Name:        violationsMetricName,
			Measure:     violationsM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{enforcementActionKey},
		},
		{
			Name:        auditDurationMetricName,
			Measure:     auditDurationM,
			Aggregation: view.Distribution(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 2, 3, 4, 5),
		},
	}
	return view.Register(views...)
}

func (r *reporter) ReportTotalViolations(enforcementAction util.EnforcementAction, v int64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(enforcementActionKey, string(enforcementAction)))
	if err != nil {
		return err
	}

	return r.report(ctx, violationsM.M(v))
}

func (r *reporter) ReportLatency(d time.Duration) error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, auditDurationM.M(d.Seconds()))
}

// StatsReporter reports audit metrics
type StatsReporter interface {
	ReportTotalViolations(enforcementAction util.EnforcementAction, v int64) error
	ReportLatency(d time.Duration) error
}

// newStatsReporter creates a reporter for audit metrics
func newStatsReporter() (StatsReporter, error) {
	ctx, err := tag.New(
		context.Background(),
	)
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx}, nil
}

type reporter struct {
	ctx context.Context
}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	return metrics.Record(ctx, m)
}
