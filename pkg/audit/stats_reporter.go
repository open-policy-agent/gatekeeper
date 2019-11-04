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
	totalViolationsName  = "violations_total"
	constraintsTotalName = "constraints_total"
	auditLatency         = "audit_latency"
	methodType           = "audit"
)

var (
	violationsTotalM  = stats.Int64(totalViolationsName, "Total number of violations per constraint", stats.UnitNone)
	constraintsTotalM = stats.Int64(constraintsTotalName, "Total number of enforced constraints", stats.UnitNone)
	auditLatencyM     = stats.Float64(auditLatency, "Latency of audit operation", stats.UnitMilliseconds)

	methodTypeKey     = tag.MustNewKey("method_type")
	constraintKindKey = tag.MustNewKey("constraint_kind")
	constraintNameKey = tag.MustNewKey("constraint_name")
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
			TagKeys:     []tag.Key{methodTypeKey, constraintKindKey, constraintNameKey},
		},
		{
			Name:        constraintsTotalName,
			Measure:     constraintsTotalM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{methodTypeKey, constraintKindKey},
		},
		{
			Name:        auditLatency,
			Measure:     auditLatencyM,
			Aggregation: view.Distribution(1000, 2000, 3000, 4000, 5000, 6000, 7000, 8000, 9000, 10000, 11000, 12000, 13000, 14000, 15000),
			TagKeys:     []tag.Key{methodTypeKey},
		},
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
}

func (r *reporter) ReportTotalViolations(constraintKind, constraintName string, v int64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(methodTypeKey, methodType),
		tag.Insert(constraintKindKey, constraintKind),
		tag.Insert(constraintNameKey, constraintName))
	if err != nil {
		return err
	}

	return r.report(ctx, violationsTotalM.M(v))
}

func (r *reporter) ReportConstraints(constraintKind string, v int64) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(methodTypeKey, methodType),
		tag.Insert(constraintKindKey, constraintKind))
	if err != nil {
		return err
	}

	return r.report(ctx, constraintsTotalM.M(v))
}

func (r *reporter) ReportLatency(d time.Duration) error {
	ctx, err := tag.New(
		r.ctx,
		tag.Insert(methodTypeKey, methodType))
	if err != nil {
		return err
	}

	// Convert time.Duration in nanoseconds to milliseconds
	return r.report(ctx, auditLatencyM.M(float64(d/time.Millisecond)))
}

// StatsReporter reports audit metrics
type StatsReporter interface {
	ReportTotalViolations(constraintKind, constraintName string, v int64) error
	ReportConstraints(constraintKind string, v int64) error
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
