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

	views = []*view.View{
		{
			Name:        lastRestart,
			Measure:     lastRestartM,
			Description: "The epoch timestamp of the last time the watch manager has restarted",
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
			Description: "The epoch timestamp of the last time the watch manager was checked for a restart condition. This is a heartbeat that should occur regularly",
			Aggregation: view.LastValue(),
		},
		{
			Name:        gvkCount,
			Measure:     gvkCountM,
			Description: "The total number of Group/Version/Kinds currently watched by the watch manager",
			Aggregation: view.LastValue(),
		},
		{
			Name:        gvkIntentCount,
			Measure:     gvkIntentCountM,
			Description: "The total number of Group/Version/Kinds that the watch manager has instructions to watch. This could differ from the actual count due to resources being pending, non-existent, or a failure of the watch manager to restart",
			Aggregation: view.LastValue(),
		},
		{
			Name:        isRunning,
			Measure:     isRunningM,
			Description: "Whether the watch manager is running. This is expected to be 1 the majority of the time with brief periods of downtime due to the watch manager being paused or restarted",
			Aggregation: view.LastValue(),
		},
	}
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

func register() error {
	return view.Register(views...)
}

func reset() error {
	view.Unregister(views...)
	return register()
}

// now returns the timestamp as a second-denominated float
func now() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func (r *reporter) reportRestartCheck() error {
	return metrics.Record(r.ctx, lastRestartCheckM.M(r.now()))
}

func (r *reporter) reportRestart() error {
	return metrics.Record(r.ctx, lastRestartM.M(r.now()))
}

func (r *reporter) reportGvkCount(count int64) error {
	return metrics.Record(r.ctx, gvkCountM.M(count))
}

func (r *reporter) reportGvkIntentCount(count int64) error {
	return metrics.Record(r.ctx, gvkIntentCountM.M(count))
}

func (r *reporter) reportIsRunning(running int64) error {
	return metrics.Record(r.ctx, isRunningM.M(running))
}

// newStatsReporter creates a reporter for watch metrics
func newStatsReporter() (*reporter, error) {
	ctx, err := tag.New(
		context.TODO(),
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
