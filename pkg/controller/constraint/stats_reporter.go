package constraint

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	totalConstraintsName = "total_constraints"
)

var (
	constraintsTotalM = stats.Int64(totalConstraintsName, "Total number of enforced constraints", stats.UnitDimensionless)

	enforcementActionKey = tag.MustNewKey("enforcement_action")
)

func init() {
	register()
}

func register() {
	views := []*view.View{
		{
			Name:        totalConstraintsName,
			Measure:     constraintsTotalM,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{enforcementActionKey},
		},
	}

	if err := view.Register(views...); err != nil {
		panic(err)
	}
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

// StatsReporter reports audit metrics
type StatsReporter interface {
	ReportConstraints(enforcementAction string, v int64) error
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
