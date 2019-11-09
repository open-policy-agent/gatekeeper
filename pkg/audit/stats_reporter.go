package audit

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	totalViolationsName  = "total_violations"
	totalConstraintsName = "total_constraints"
	auditDuration        = "audit_duration"
)

var (
	violationsTotalM  = stats.Int64(totalViolationsName, "Total number of violations per constraint", stats.UnitDimensionless)
	constraintsTotalM = stats.Int64(totalConstraintsName, "Total number of enforced constraints", stats.UnitDimensionless)
	auditDurationM    = stats.Float64(auditDuration, "Latency of audit operation in seconds", stats.UnitSeconds)

	enforcementActionKey = tag.MustNewKey("enforcement_action")
)

func init() {
	register()
}

func register() {
	views := []*view.View{
		{
			Name:        totalViolationsName,
			Measure:     violationsTotalM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{enforcementActionKey},
		},
		{
			Name:        totalConstraintsName,
			Measure:     constraintsTotalM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{enforcementActionKey},
		},
		{
			Name:        auditDuration,
			Measure:     auditDurationM,
			Aggregation: view.Distribution(1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000, 11000, 12000, 13000, 14000, 15000),
		},
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
}

func (r *reporter) ReportTotalViolations(enforcementAction string, v int64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(enforcementActionKey, enforcementAction))
	if err != nil {
		return err
	}

	return r.report(ctx, violationsTotalM.M(v))
}

func (r *reporter) ReportConstraints(enforcementAction string, v int64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(enforcementActionKey, enforcementAction))
	if err != nil {
		return err
	}

	return r.report(ctx, constraintsTotalM.M(v))
}

func (r *reporter) ReportLatency(d time.Duration) error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	// Convert time.Duration in nanoseconds to seconds
	return r.report(ctx, auditDurationM.M(float64(d/time.Second)))
}

// StatsReporter reports audit metrics
type StatsReporter interface {
	ReportTotalViolations(enforcementAction string, v int64) error
	ReportConstraints(enforcementAction string, v int64) error
	ReportLatency(d time.Duration) error
}

// NewStatsReporter creaters a reporter for audit metrics
func NewStatsReporter() (StatsReporter, error) {
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
	metrics.Record(ctx, m)
	return nil
}
