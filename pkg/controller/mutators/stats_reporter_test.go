package mutators

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

func initializeTestInstruments(t *testing.T) (rdr *sdkmetric.PeriodicReader, r StatsReporter) {
	var err error
	rdr = sdkmetric.NewPeriodicReader(new(testmetric.FnExporter))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	r = NewStatsReporter()
	meter := mp.Meter("test")
	responseTimeInSecM, err = meter.Float64Histogram(mutatorIngestionDurationMetricName)
	assert.NoError(t, err)
	mutatorIngestionCountM, err = meter.Int64Counter(mutatorIngestionCountMetricName)
	assert.NoError(t, err)
	reporterInstance, ok := r.(*reporter)
	assert.True(t, ok, "Failed to assert type *reporter")
	_, err = meter.Int64ObservableGauge(mutatorsMetricName, metric.WithInt64Callback(reporterInstance.observeMutatorsStatus))
	assert.NoError(t, err)
	_, err = meter.Int64ObservableGauge(mutatorsConflictingCountMetricsName, metric.WithInt64Callback(reporterInstance.observeMutatorsInConflict))
	assert.NoError(t, err)

	return rdr, r
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

func TestReportMutatorIngestionRequest(t *testing.T) {
	var err error
	const (
		minIngestDuration = 1 * time.Second
		maxIngestDuration = 5 * time.Second
	)

	want1 := metricdata.Metrics{
		Name: mutatorIngestionDurationMetricName,
		Data: metricdata.Histogram[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[float64]{
				{
					Attributes:   attribute.NewSet(attribute.String(statusKey, string(MutatorStatusActive))),
					Count:        2,
					Bounds:       []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000},
					BucketCounts: []uint64{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[float64](1.),
					Max:          metricdata.NewExtrema[float64](5.),
					Sum:          6,
				},
			},
		},
	}
	want2 := metricdata.Metrics{
		Name: mutatorIngestionCountMetricName,
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(statusKey, string(MutatorStatusActive))), Value: 2},
			},
			IsMonotonic: true,
		},
	}

	rdr, r := initializeTestInstruments(t)

	ctx := context.Background()

	err = r.ReportMutatorIngestionRequest(MutatorStatusActive, minIngestDuration)
	assert.NoError(t, err)

	err = r.ReportMutatorIngestionRequest(MutatorStatusActive, maxIngestDuration)
	assert.NoError(t, err)

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(ctx, rm))

	metricdatatest.AssertEqual(t, want1, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())
	metricdatatest.AssertEqual(t, want2, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
}

func TestReportMutatorsStatus(t *testing.T) {
	// Set up some test data.
	mID := types.ID{
		Group:     "test",
		Kind:      "test",
		Name:      "test",
		Namespace: "test",
	}
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		c           *Cache
	}{
		{
			name:        "reporting mutator status",
			ctx:         context.Background(),
			expectedErr: nil,
			c: &Cache{
				cache: map[types.ID]mutatorStatus{
					mID: {
						ingestion: MutatorStatusActive,
						conflict:  true,
					},
				},
			},
			want: metricdata.Metrics{
				Name: mutatorsMetricName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Attributes: attribute.NewSet(attribute.String(statusKey, string(MutatorStatusActive))), Value: 1},
						{Attributes: attribute.NewSet(attribute.String(statusKey, string(MutatorStatusError))), Value: 0},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			r.RegisterTally(tt.c.TallyStatus, tt.c.TallyConflict)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, findMetricByName(t, rm, mutatorsMetricName), metricdatatest.IgnoreTimestamp())
		})
	}
}

func TestReporterAggregatesAcrossRegisteredCaches(t *testing.T) {
	rdr, r := initializeTestInstruments(t)

	assignCache := NewMutationCache()
	modifySetCache := NewMutationCache()
	assignMetaCache := NewMutationCache()

	r.RegisterTally(assignCache.TallyStatus, assignCache.TallyConflict)
	r.RegisterTally(modifySetCache.TallyStatus, modifySetCache.TallyConflict)
	r.RegisterTally(assignMetaCache.TallyStatus, assignMetaCache.TallyConflict)

	for i := 0; i < 4; i++ {
		assignCache.Upsert(types.ID{Group: "mutations.gatekeeper.sh", Kind: "Assign", Name: fmt.Sprintf("assign-%d", i)}, MutatorStatusActive, false)
	}
	for i := 0; i < 2; i++ {
		modifySetCache.Upsert(types.ID{Group: "mutations.gatekeeper.sh", Kind: "ModifySet", Name: fmt.Sprintf("modifyset-%d", i)}, MutatorStatusActive, false)
	}
	assignMetaCache.Upsert(types.ID{Group: "mutations.gatekeeper.sh", Kind: "AssignMetadata", Name: "assignmeta-0"}, MutatorStatusActive, false)

	want := metricdata.Metrics{
		Name: mutatorsMetricName,
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attribute.String(statusKey, string(MutatorStatusActive))), Value: 7},
				{Attributes: attribute.NewSet(attribute.String(statusKey, string(MutatorStatusError))), Value: 0},
			},
		},
	}

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, findMetricByName(t, rm, mutatorsMetricName), metricdatatest.IgnoreTimestamp())
}

func TestReporterAggregatesConflictsAcrossCaches(t *testing.T) {
	rdr, r := initializeTestInstruments(t)

	cache1 := NewMutationCache()
	cache2 := NewMutationCache()
	r.RegisterTally(cache1.TallyStatus, cache1.TallyConflict)
	r.RegisterTally(cache2.TallyStatus, cache2.TallyConflict)

	// 1 conflict in cache1, 2 conflicts in cache2
	cache1.Upsert(types.ID{Kind: "Assign", Name: "a1"}, MutatorStatusActive, true)
	cache2.Upsert(types.ID{Kind: "ModifySet", Name: "m1"}, MutatorStatusActive, true)
	cache2.Upsert(types.ID{Kind: "ModifySet", Name: "m2"}, MutatorStatusActive, true)

	want := metricdata.Metrics{
		Name: mutatorsConflictingCountMetricsName,
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{Value: 3},
			},
		},
	}

	rm := &metricdata.ResourceMetrics{}
	assert.NoError(t, rdr.Collect(context.Background(), rm))
	metricdatatest.AssertEqual(t, want, findMetricByName(t, rm, mutatorsConflictingCountMetricsName), metricdatatest.IgnoreTimestamp())
}

func TestReportMutatorsInConflict(t *testing.T) {
	mID := types.ID{
		Group:     "test",
		Kind:      "test",
		Name:      "test",
		Namespace: "test",
	}
	tests := []struct {
		name        string
		ctx         context.Context
		expectedErr error
		want        metricdata.Metrics
		c           *Cache
	}{
		{
			name:        "reporting mutator conflict status",
			ctx:         context.Background(),
			expectedErr: nil,
			c: &Cache{
				cache: map[types.ID]mutatorStatus{
					mID: {
						ingestion: MutatorStatusActive,
						conflict:  true,
					},
				},
			},
			want: metricdata.Metrics{
				Name: mutatorsConflictingCountMetricsName,
				Data: metricdata.Gauge[int64]{
					DataPoints: []metricdata.DataPoint[int64]{
						{Value: 1},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdr, r := initializeTestInstruments(t)
			r.RegisterTally(tt.c.TallyStatus, tt.c.TallyConflict)

			rm := &metricdata.ResourceMetrics{}
			assert.Equal(t, tt.expectedErr, rdr.Collect(tt.ctx, rm))

			metricdatatest.AssertEqual(t, tt.want, findMetricByName(t, rm, mutatorsConflictingCountMetricsName), metricdatatest.IgnoreTimestamp())
		})
	}
}
