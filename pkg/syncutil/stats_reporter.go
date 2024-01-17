package syncutil

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("reporter").WithValues("metaKind", "Sync")

const (
	syncMetricName         = "sync"
	syncDurationMetricName = "sync_duration_seconds"
	lastRunTimeMetricName  = "sync_last_run_time"
	kindKey                = "kind"
	statusKey              = "status"
)

var (
	syncDurationM metric.Float64Histogram
	r             *Reporter
)

type MetricsCache struct {
	mux        sync.RWMutex
	KnownKinds map[string]bool
	Cache      map[string]Tags
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

func GetKeyForSyncMetrics(namespace string, name string) string {
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

func (c *MetricsCache) GetTags(key string) *Tags {
	c.mux.RLock()
	defer c.mux.RUnlock()

	cpy := &Tags{}
	v, ok := c.Cache[key]
	if ok {
		cpy.Kind = v.Kind
		cpy.Status = v.Status
	}

	return cpy
}

func (c *MetricsCache) HasObject(key string) bool {
	c.mux.RLock()
	defer c.mux.RUnlock()

	_, ok := c.Cache[key]
	return ok
}

func (c *MetricsCache) ReportSync() {
	c.mux.RLock()
	defer c.mux.RUnlock()

	reporter, err := NewStatsReporter()
	if err != nil {
		log.Error(err, "failed to initialize reporter")
		return
	}

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

type Reporter struct {
	mu         sync.RWMutex
	lastRun    float64
	syncReport map[Tags]int64
	now        func() float64
}

// NewStatsReporter creates a reporter for sync metrics.
func NewStatsReporter() (*Reporter, error) {
	if r == nil {
		var err error
		meter := otel.GetMeterProvider().Meter("gatekeeper")
		r = &Reporter{now: now}

		_, err = meter.Int64ObservableGauge(syncMetricName, metric.WithDescription("Total number of resources of each kind being cached"), metric.WithInt64Callback(r.observeSync))
		if err != nil {
			return nil, err
		}
		syncDurationM, err = meter.Float64Histogram(syncDurationMetricName, metric.WithDescription("Latency of sync operation in seconds"), metric.WithUnit("s"))
		if err != nil {
			return nil, err
		}
		_, err = meter.Float64ObservableGauge(lastRunTimeMetricName, metric.WithDescription("Timestamp of last sync operation"), metric.WithUnit("s"), metric.WithFloat64Callback(r.observeLastSync))
		if err != nil {
			return nil, err
		}

		view.Register(
			sdkmetric.NewView(
				sdkmetric.Instrument{Name: syncDurationMetricName},
				sdkmetric.Stream{
					Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
						Boundaries: []float64{0.0001, 0.0002, 0.0003, 0.0004, 0.0005, 0.0006, 0.0007, 0.0008, 0.0009, 0.001, 0.002, 0.003, 0.004, 0.005, 0.01, 0.02, 0.03, 0.04, 0.05},
					},
				},
			))
	}
	return r, nil
}

func (r *Reporter) ReportSyncDuration(d time.Duration) error {
	ctx := context.Background()
	syncDurationM.Record(ctx, d.Seconds())
	return nil
}

func (r *Reporter) ReportLastSync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastRun = r.now()
	return nil
}

func (r *Reporter) ReportSync(t Tags, v int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.syncReport == nil {
		r.syncReport = make(map[Tags]int64)
	}
	r.syncReport[t] = v
	return nil
}

func (r *Reporter) observeLastSync(_ context.Context, observer metric.Float64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	observer.Observe(r.lastRun)
	return nil
}

func (r *Reporter) observeSync(_ context.Context, observer metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for t, v := range r.syncReport {
		observer.Observe(v, metric.WithAttributes(attribute.String(kindKey, t.Kind), attribute.String(statusKey, string(t.Status))))
	}
	return nil
}

// now returns the timestamp as a second-denominated float.
func now() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
