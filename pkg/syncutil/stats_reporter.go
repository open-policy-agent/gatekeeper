package syncutil

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	syncMetricName         = "sync"
	syncDurationMetricName = "sync_duration_seconds"
	lastRunTimeMetricName  = "sync_last_run_time"
	kindKey                = "kind"
	statusKey              = "status"
)

var (
	syncM         metric.Int64ObservableGauge
	syncDurationM metric.Float64Histogram
	lastRunSyncM  metric.Float64ObservableGauge
	meter         metric.Meter
)

func init() {
	var err error
	meter = otel.GetMeterProvider().Meter("gatekeeper")

	syncM, err = meter.Int64ObservableGauge(syncMetricName, metric.WithDescription("Total number of resources of each kind being cached"))
	if err != nil {
		panic(err)
	}
	syncDurationM, err = meter.Float64Histogram(syncDurationMetricName, metric.WithDescription("Latency of sync operation in seconds"), metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
	lastRunSyncM, err = meter.Float64ObservableGauge(lastRunTimeMetricName, metric.WithDescription("Timestamp of last sync operation"), metric.WithUnit("s"))
	if err != nil {
		panic(err)
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

type Reporter struct {
	now func() float64
}

// NewStatsReporter creates a reporter for sync metrics.
func NewStatsReporter() (*Reporter, error) {
	return &Reporter{now: now}, nil
}

func (r *Reporter) RegisterCallback(c *MetricsCache) error {
	_, err1 := meter.RegisterCallback(c.ReportSync, syncM)
	_, err2 := meter.RegisterCallback(r.ReportLastSync, lastRunSyncM)
	return errors.Join(err1, err2)
}

func (r *Reporter) ReportSyncDuration(d time.Duration) error {
	ctx := context.Background()
	syncDurationM.Record(ctx, d.Seconds())
	return nil
}

func (r *Reporter) ReportLastSync(_ context.Context, observer metric.Observer) error {
	observer.ObserveFloat64(lastRunSyncM, r.now())
	return nil
}

func (c *MetricsCache) ReportSync(_ context.Context, observer metric.Observer) error {
	c.mux.RLock()
	defer c.mux.RUnlock()

	totals := make(map[Tags]int)
	for _, v := range c.Cache {
		totals[v]++
	}

	for kind := range c.KnownKinds {
		for _, status := range metrics.AllStatuses {
			t := Tags{
				Kind:   kind,
				Status: status,
			}
			v := int64(totals[Tags{
				Kind:   kind,
				Status: status,
			}])
			observer.ObserveInt64(syncM, v, metric.WithAttributes(attribute.String(kindKey, t.Kind), attribute.String(statusKey, string(t.Status))))
		}
	}
	return nil
}

// now returns the timestamp as a second-denominated float.
func now() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
