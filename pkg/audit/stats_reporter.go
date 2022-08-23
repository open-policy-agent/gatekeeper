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
	violationsMetricName       = "violations"
	auditDurationMetricName    = "audit_duration_seconds"
	lastRunStartTimeMetricName = "audit_last_run_time"
	lastRunEndTimeMetricName   = "audit_last_run_end_time"
)

var (
	violationsM       = stats.Int64(violationsMetricName, "Total number of audited violations", stats.UnitDimensionless)
	auditDurationM    = stats.Float64(auditDurationMetricName, "Latency of audit operation in seconds", stats.UnitSeconds)
	lastRunStartTimeM = stats.Float64(lastRunStartTimeMetricName, "Timestamp of last audit run starting time", stats.UnitSeconds)
	lastRunEndTimeM   = stats.Float64(lastRunEndTimeMetricName, "Timestamp of last audit run ending time", stats.UnitSeconds)

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
			Aggregation: view.Distribution(1*60, 3*60, 5*60, 10*60, 15*60, 20*60, 40*60, 80*60, 160*60, 320*60),
		},
		{
			Name:        lastRunStartTimeMetricName,
			Measure:     lastRunStartTimeM,
			Aggregation: view.LastValue(),
		},
		{
			Name:        lastRunEndTimeMetricName,
			Measure:     lastRunEndTimeM,
			Aggregation: view.LastValue(),
		},
	}
	return view.Register(views...)
}

func (r *reporter) reportTotalViolations(enforcementAction util.EnforcementAction, v int64) error {
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(enforcementActionKey, string(enforcementAction)))
	if err != nil {
		return err
	}

	return r.report(ctx, violationsM.M(v))
}

func (r *reporter) reportLatency(d time.Duration) error {
	ctx, err := tag.New(context.Background())
	if err != nil {
		return err
	}

	return r.report(ctx, auditDurationM.M(d.Seconds()))
}

func (r *reporter) reportRunStart(t time.Time) error {
	ctx, err := tag.New(context.Background())
	if err != nil {
		return err
	}

	val := float64(t.Unix())
	return metrics.Record(ctx, lastRunStartTimeM.M(val))
}

func (r *reporter) reportRunEnd(t time.Time) error {
	ctx, err := tag.New(context.Background())
	if err != nil {
		return err
	}

	val := float64(t.Unix())
	return metrics.Record(ctx, lastRunEndTimeM.M(val))
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	return &reporter{}, nil
}

type reporter struct{}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	return metrics.Record(ctx, m)
}
