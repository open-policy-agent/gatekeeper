package syncutil

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r *Reporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	r = &Reporter{now: now}
	_, err = meter.Int64ObservableGauge(syncMetricName, metric.WithInt64Callback(r.observeSync))
	assert.NoError(t, err)
	syncDurationM, err = meter.Float64Histogram(syncDurationMetricName)
	assert.NoError(t, err)
	_, err = meter.Float64ObservableGauge(lastRunTimeMetricName, metric.WithFloat64Callback(r.observeLastSync))
	assert.NoError(t, err)

	return rdr, r
}

func useStatsReporter(t *testing.T, reporter *Reporter) {
	t.Helper()

	oldReporter := r
	r = reporter
	t.Cleanup(func() {
		r = oldReporter
	})
}

func findMetricByName(t *testing.T, rm *metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	var (
		result metricdata.Metrics
		found  bool
	)

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if found {
					t.Fatalf("multiple metrics found with name %q", name)
				}
				result = m
				found = true
			}
		}
	}

	if !found {
		t.Fatalf("metric %q not found", name)
	}

	return result
}

func reporterSyncReport(t *testing.T, reporter *Reporter) map[Tags]int64 {
	t.Helper()

	reporter.mu.RLock()
	defer reporter.mu.RUnlock()

	result := make(map[Tags]int64, len(reporter.syncReport))
	for tags, count := range reporter.syncReport {
		result[tags] = count
	}

	return result
}

func TestReportSync(t *testing.T) {
	wantTags := Tags{
		Kind:   "Pod",
		Status: metrics.ActiveStatus,
	}
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		c           *MetricsCache
	}{
		{
			name:        "reporting sync",
			ctx:         context.Background(),
			expectedErr: nil,
			c: func() *MetricsCache {
				c := NewMetricsCache()
				c.AddKind("Pod")
				c.AddObject("Pod", wantTags)
				return c
			}(),
			want: metricdata.Metrics{
				Name: syncMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(kindKey, wantTags.Kind), attribute.String(statusKey, string(wantTags.Status))), Value: 1},
						{Attributes: attribute.NewSet(attribute.String(kindKey, wantTags.Kind), attribute.String(statusKey, string(metrics.ErrorStatus))), Value: 0},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			useStatsReporter(t, r)
			tt.c.ReportSync()

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))
			metricdatatest.AssertEqual(t, tt.want, findMetricByName(t, rm, syncMetricName), metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestMetricsCacheReportSyncTracksTotals(t *testing.T) {
	_, reporter := initializeTestInstruments(t)
	useStatsReporter(t, reporter)

	c := NewMetricsCache()
	c.AddKind("Pod")
	c.AddKind("Namespace")

	c.AddObject(GetKeyForSyncMetrics("default", "pod-a"), Tags{Kind: "Pod", Status: metrics.ActiveStatus})
	c.AddObject(GetKeyForSyncMetrics("default", "pod-b"), Tags{Kind: "Pod", Status: metrics.ActiveStatus})
	c.AddObject(GetKeyForSyncMetrics("default", "pod-b"), Tags{Kind: "Pod", Status: metrics.ErrorStatus})
	c.AddObject(GetKeyForSyncMetrics("", "namespace-a"), Tags{Kind: "Namespace", Status: metrics.ActiveStatus})
	c.DeleteObject(GetKeyForSyncMetrics("", "namespace-a"))
	c.DeleteObject(GetKeyForSyncMetrics("default", "missing"))

	c.ReportSync()

	assert.Equal(t, map[Tags]int64{
		{Kind: "Pod", Status: metrics.ActiveStatus}:       1,
		{Kind: "Pod", Status: metrics.ErrorStatus}:        1,
		{Kind: "Namespace", Status: metrics.ActiveStatus}: 0,
		{Kind: "Namespace", Status: metrics.ErrorStatus}:  0,
	}, reporterSyncReport(t, reporter))

	c.ResetCache()
	c.ReportSync()

	assert.Equal(t, map[Tags]int64{
		{Kind: "Pod", Status: metrics.ActiveStatus}:       0,
		{Kind: "Pod", Status: metrics.ErrorStatus}:        0,
		{Kind: "Namespace", Status: metrics.ActiveStatus}: 0,
		{Kind: "Namespace", Status: metrics.ErrorStatus}:  0,
	}, reporterSyncReport(t, reporter))
}

func TestMetricsCacheConcurrentAccess(t *testing.T) {
	_, reporter := initializeTestInstruments(t)
	useStatsReporter(t, reporter)

	c := NewMetricsCache()
	c.AddKind("Pod")

	const workers = 8
	const objectsPerWorker = 100

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()

			for i := 0; i < objectsPerWorker; i++ {
				key := GetKeyForSyncMetrics("default", fmt.Sprintf("pod-%d-%d", worker, i))
				c.AddObject(key, Tags{Kind: "Pod", Status: metrics.ActiveStatus})
				c.ReportSync()
				if i%2 == 0 {
					c.AddObject(key, Tags{Kind: "Pod", Status: metrics.ErrorStatus})
				}
				if i%4 == 0 {
					c.DeleteObject(key)
				}
			}
		}(worker)
	}
	wg.Wait()

	c.ReportSync()
	report := reporterSyncReport(t, reporter)

	assert.Equal(t, int64(400), report[Tags{Kind: "Pod", Status: metrics.ActiveStatus}])
	assert.Equal(t, int64(200), report[Tags{Kind: "Pod", Status: metrics.ErrorStatus}])
}

func TestReportSyncLatency(t *testing.T) {
	const minLatency = 100 * time.Second
	const maxLatency = 500 * time.Second
	const wantLatencyCount uint64 = 2
	const wantLatencyMin float64 = 100
	const wantLatencyMax float64 = 500

	want := metricdata.Metrics{
		Name: syncDurationMetricName,
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.Set{},
					Count:        wantLatencyCount,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](wantLatencyMin),
					Max:          metricdata.NewExtrema[float64](wantLatencyMax),
					Sum:          600,
				},
			},
		},
	}
	rdr, r := initializeTestInstruments(t)

	assert.NoError(t, r.ReportSyncDuration(minLatency))

	assert.NoError(t, r.ReportSyncDuration(maxLatency))

	rm := &metricdata.ResourceMetrics{}
	assert.Equal(t, nil, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
}

func TestLastRunSync(t *testing.T) {
	const wantTime float64 = 11

	fakeNow := func() float64 {
		return wantTime
	}

	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
	}{
		{
			name:        "reporting last sync run",
			ctx:         context.Background(),
			expectedErr: nil,
			want: metricdata.Metrics{
				Name: lastRunTimeMetricName,
				Data: metricdata.Gauge[float64]{
					DataPoints: []metricdata.DataPoint[float64]{
						{Value: wantTime},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			r.now = fakeNow
			assert.NoError(t, r.ReportLastSync())

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
		})
	}
}

func BenchmarkMetricsCacheReportSync(b *testing.B) {
	for _, objects := range []int{100, 10000, 100000} {
		b.Run("cached_totals/objects="+strconv.Itoa(objects), func(b *testing.B) {
			useBenchmarkStatsReporter(b)
			c := newBenchmarkMetricsCache(objects)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.ReportSync()
			}
		})

		b.Run("scan_cache/objects="+strconv.Itoa(objects), func(b *testing.B) {
			useBenchmarkStatsReporter(b)
			c := newBenchmarkMetricsCache(objects)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reportSyncByScanning(c)
			}
		})
	}
}

func useBenchmarkStatsReporter(b *testing.B) {
	b.Helper()

	oldReporter := r
	r = &Reporter{
		now:        now,
		syncReport: make(map[Tags]int64),
	}
	b.Cleanup(func() {
		r = oldReporter
	})
}

func newBenchmarkMetricsCache(objects int) *MetricsCache {
	const kinds = 4

	c := NewMetricsCache()
	kindNames := make([]string, 0, kinds)
	for i := 0; i < kinds; i++ {
		kind := "Kind" + strconv.Itoa(i)
		c.AddKind(kind)
		kindNames = append(kindNames, kind)
	}

	for i := 0; i < objects; i++ {
		status := metrics.ActiveStatus
		if i%2 == 0 {
			status = metrics.ErrorStatus
		}

		kind := kindNames[i%len(kindNames)]
		c.AddObject(GetKeyForSyncMetrics("default", kind+"-"+strconv.Itoa(i)), Tags{Kind: kind, Status: status})
	}

	return c
}

func reportSyncByScanning(c *MetricsCache) {
	reporter, err := NewStatsReporter()
	if err != nil {
		log.Error(err, "failed to initialize reporter")
		return
	}

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
