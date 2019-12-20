package watch

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	lastRestart      = "watch_manager_last_restart_time"
	lastRestartCheck = "watch_manager_last_restart_check_time"
	totalRestarts    = "watch_manager_total_restart_attempts"
	gvkCount         = "watch_manager_total_watched_gvk"
	gvkIntentCount   = "watch_manager_total_intended_watch_gvk"
	isRunning        = "watch_manager_is_running"
)

var (
	lastRestartM      = stats.Float64(lastRestart, "Timestamp of last watch manager restart", stats.UnitSeconds)
	lastRestartCheckM = stats.Float64(lastRestartCheck, "Timestamp of last time watch manager checked if it needed to restart", stats.UnitSeconds)
	gvkCountM         = stats.Int64(gvkCount, "Total number of watched GroupVersionKinds", stats.UnitDimensionless)
	gvkIntentCountM   = stats.Int64(gvkIntentCount, "Total number of GroupVersionKinds with a registered watch intent", stats.UnitDimensionless)
	isRunningM        = stats.Int64(isRunning, "One if the watch manager is running, zero if not", stats.UnitDimensionless)
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

func register() error {
	views := []*view.View{
		{
			Name:        lastRestart,
			Measure:     lastRestartM,
			Aggregation: view.LastValue(),
		},
		{
			Name:        totalRestarts,
			Measure:     lastRestartM,
			Description: "Total number of times the watch manager has restarted",
			Aggregation: view.Count(),
		},
		{
			Name:        lastRestartCheck,
			Measure:     lastRestartCheckM,
			Aggregation: view.LastValue(),
		},
		{
			Name:        gvkCount,
			Measure:     gvkCountM,
			Aggregation: view.LastValue(),
		},
		{
			Name:        gvkIntentCount,
			Measure:     gvkIntentCountM,
			Aggregation: view.LastValue(),
		},
		{
			Name:        isRunning,
			Measure:     isRunningM,
			Aggregation: view.LastValue(),
		},
	}
	return view.Register(views...)
}

// now returns the timestamp as a second-denominated float
func now() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func (r *reporter) reportRestartCheck() error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, lastRestartCheckM.M(r.now()))
}

func (r *reporter) reportRestart() error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, lastRestartM.M(r.now()))
}

func (r *reporter) reportGvkCount(count int64) error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, gvkCountM.M(count))
}

func (r *reporter) reportGvkIntentCount(count int64) error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, gvkIntentCountM.M(count))
}

func (r *reporter) reportIsRunning(running int64) error {
	ctx, err := tag.New(r.ctx)
	if err != nil {
		return err
	}

	return r.report(ctx, isRunningM.M(running))
}

// newStatsReporter creates a reporter for watch metrics
func newStatsReporter() (*reporter, error) {
	ctx, err := tag.New(
		context.Background(),
	)
	if err != nil {
		return nil, err
	}

	return &reporter{ctx: ctx, now: now}, nil
}

type reporter struct {
	ctx context.Context
	now func() float64
}

func (r *reporter) report(ctx context.Context, m stats.Measurement) error {
	return metrics.Record(ctx, m)
}
