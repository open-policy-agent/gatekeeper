package syncutil

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	syncMetricName         = "sync"
	syncDurationMetricName = "sync_duration_seconds"
	lastRunTimeMetricName  = "sync_last_run_time"
)

var (
	syncM         = stats.Int64(syncMetricName, "Total number of resources of each kind being cached", stats.UnitDimensionless)
	syncDurationM = stats.Float64(syncDurationMetricName, "Latency of sync operation in seconds", stats.UnitSeconds)
	lastRunSyncM  = stats.Float64(lastRunTimeMetricName, "Timestamp of last sync operation", stats.UnitSeconds)

	kindKey   = tag.MustNewKey("kind")
	statusKey = tag.MustNewKey("status")

	views = []*view.View{
		{
			Name:        syncM.Name(),
			Measure:     syncM,
			Description: syncM.Description(),
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{kindKey, statusKey},
		},
		{
			Name:        syncDurationM.Name(),
			Measure:     syncDurationM,
			Description: syncDurationM.Description(),
			Aggregation: view.Distribution(0.0001, 0.0002, 0.0003, 0.0004, 0.0005, 0.0006, 0.0007, 0.0008, 0.0009, 0.001, 0.002, 0.003, 0.004, 0.005, 0.01, 0.02, 0.03, 0.04, 0.05),
		},
		{
			Name:        lastRunSyncM.Name(),
			Measure:     lastRunSyncM,
			Description: lastRunSyncM.Description(),
			Aggregation: view.LastValue(),
		},
	}
)

type MetricsCache struct {
	mux        sync.RWMutex // todo FRICTION, gate metrics cache under mux from cmt
	Cache      map[string]Tags
	KnownKinds map[string]bool
}

type Tags struct {
	Kind   string
	Status metrics.Status
}

func NewMetricsCache() *MetricsCache {
	return &MetricsCache{
		Cache:      make(map[string]Tags),
		KnownKinds: make(map[string]bool),
	}
}

func (c *MetricsCache) GetSyncKey(namespace string, name string) string {
	return strings.Join([]string{namespace, name}, "/")
}

// need to know encountered kinds to reset metrics for that kind
// this is a known memory leak
// footprint should naturally reset on Pod upgrade b/c the container restarts.
func (c *MetricsCache) AddKind(key string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.KnownKinds[key] = true
}

func (c *MetricsCache) ResetCache() {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.Cache = make(map[string]Tags)
}

func (c *MetricsCache) AddObject(key string, t Tags) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.Cache[key] = Tags{
		Kind:   t.Kind,
		Status: t.Status,
	}
}

func (c *MetricsCache) DeleteObject(key string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.Cache, key)
}

func (c *MetricsCache) ReportSync(reporter *Reporter, log logr.Logger) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	totals := make(map[Tags]int)
	for _, v := range c.Cache {
		totals[v]++
	}

	for kind := range c.KnownKinds {
		for _, status := range metrics.AllStatuses {
			if err := reporter.ReportSync(
				Tags{
					Kind:   kind,
					Status: status,
				},
				int64(totals[Tags{
					Kind:   kind,
					Status: status,
				}])); err != nil {
				log.Error(err, "failed to report sync")
			}
		}
	}
}

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

func register() error {
	return view.Register(views...)
}

type Reporter struct {
	now func() float64
}

// NewStatsReporter creates a reporter for sync metrics.
func NewStatsReporter() (*Reporter, error) {
	return &Reporter{now: now}, nil
}

func (r *Reporter) ReportSyncDuration(d time.Duration) error {
	ctx := context.Background()
	return metrics.Record(ctx, syncDurationM.M(d.Seconds()))
}

func (r *Reporter) ReportLastSync() error {
	ctx := context.Background()
	return metrics.Record(ctx, lastRunSyncM.M(r.now()))
}

func (r *Reporter) ReportSync(t Tags, v int64) error {
	ctx, err := tag.New(
		context.Background(),
		tag.Insert(kindKey, t.Kind),
		tag.Insert(statusKey, string(t.Status)))
	if err != nil {
		return err
	}

	return metrics.Record(ctx, syncM.M(v))
}

// now returns the timestamp as a second-denominated float.
func now() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
