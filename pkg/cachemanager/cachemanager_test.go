package cachemanager

import (
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 2)
}

func makeCacheManager(t *testing.T) (*CacheManager, context.Context) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx, cancelFunc := context.WithCancel(context.Background())

	cfClient := &fakes.FakeCfClient{}
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	require.NoError(t, err)
	processExcluder := process.Get()
	processExcluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"sync"},
		},
	})
	events := make(chan event.GenericEvent, 1024)
	reg, err := wm.NewRegistrar(
		"test-cache-manager",
		events)
	require.NoError(t, err)

	cacheManager, err := NewCacheManager(&Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           c,
		GVKAggregator:    aggregator.NewGVKAggregator(),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		cancelFunc()
	})

	testutils.StartManager(ctx, t, mgr)

	return cacheManager, ctx
}

func TestCacheManager_wipeCacheIfNeeded(t *testing.T) {
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	dataClientForTest := func() CFDataClient {
		cfdc := &fakes.FakeCfClient{}

		cm := unstructuredFor(configMapGVK, "config-test-1")
		_, err := cfdc.AddData(context.Background(), cm)

		require.NoError(t, err, "adding ConfigMap config-test-1 in cfClient")

		return cfdc
	}

	tcs := []struct {
		name         string
		cm           *CacheManager
		expectedData map[fakes.CfDataKey]interface{}
	}{
		{
			name: "wipe cache if there are gvks to remove",
			cm: &CacheManager{
				cfClient: dataClientForTest(),
				gvksToDeleteFromCache: func() *watch.Set {
					gvksToDelete := watch.NewSet()
					gvksToDelete.Add(configMapGVK)
					return gvksToDelete
				}(),
				syncMetricsCache: syncutil.NewMetricsCache(),
			},
			expectedData: map[fakes.CfDataKey]interface{}{},
		},
		{
			name: "wipe cache if there are excluder changes",
			cm: &CacheManager{
				cfClient:              dataClientForTest(),
				excluderChanged:       true,
				syncMetricsCache:      syncutil.NewMetricsCache(),
				gvksToDeleteFromCache: watch.NewSet(),
			},
			expectedData: map[fakes.CfDataKey]interface{}{},
		},
		{
			name: "don't wipe cache if no excluder changes or no gvks to delete",
			cm: &CacheManager{
				cfClient:              dataClientForTest(),
				syncMetricsCache:      syncutil.NewMetricsCache(),
				gvksToDeleteFromCache: watch.NewSet(),
			},
			expectedData: map[fakes.CfDataKey]interface{}{{Gvk: configMapGVK, Key: "default/config-test-1"}: nil},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cfClient, ok := tc.cm.cfClient.(*fakes.FakeCfClient)
			require.True(t, ok)

			tc.cm.wipeCacheIfNeeded(context.Background())
			require.True(t, cfClient.Contains(tc.expectedData))
		})
	}
}

// TestCacheManager_AddObject tests that we can add objects in the cache.
func TestCacheManager_AddObject(t *testing.T) {
	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	mgr, _ := testutils.SetupManager(t, cfg)

	tcs := []struct {
		name                 string
		cm                   *CacheManager
		expectSyncMetric     bool
		expectedMetricStatus metrics.Status
		expectedData         map[fakes.CfDataKey]interface{}
	}{
		{
			name: "AddObject happy path",
			cm: &CacheManager{
				cfClient: &fakes.FakeCfClient{},
				watchedSet: func() *watch.Set {
					ws := watch.NewSet()
					ws.Add(pod.GroupVersionKind())

					return ws
				}(),
				tracker:          readiness.NewTracker(mgr.GetAPIReader(), false, false, false),
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder:  process.Get(),
			},
			expectedData:         map[fakes.CfDataKey]interface{}{{Gvk: pod.GroupVersionKind(), Key: "test-ns/test-name"}: nil},
			expectSyncMetric:     true,
			expectedMetricStatus: metrics.ActiveStatus,
		},
		{
			name: "AddObject has no effect if GVK is not watched",
			cm: &CacheManager{
				cfClient:         &fakes.FakeCfClient{},
				watchedSet:       watch.NewSet(),
				tracker:          readiness.NewTracker(mgr.GetAPIReader(), false, false, false),
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder:  process.Get(),
			},
			expectedData:     map[fakes.CfDataKey]interface{}{},
			expectSyncMetric: false,
		},
		{
			name: "AddObject has no effect if GVK is process excluded",
			cm: &CacheManager{
				cfClient: &fakes.FakeCfClient{},
				watchedSet: func() *watch.Set {
					ws := watch.NewSet()
					ws.Add(pod.GroupVersionKind())

					return ws
				}(),
				tracker:          readiness.NewTracker(mgr.GetAPIReader(), false, false, false),
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder: func() *process.Excluder {
					processExcluder := process.New()
					processExcluder.Add([]configv1alpha1.MatchEntry{
						{
							ExcludedNamespaces: []wildcard.Wildcard{"test-ns"},
							Processes:          []string{"sync"},
						},
					})
					return processExcluder
				}(),
			},
			expectedData:     map[fakes.CfDataKey]interface{}{},
			expectSyncMetric: false,
		},
		{
			name: "AddObject sets metrics on error from cfdataclient",
			cm: &CacheManager{
				cfClient: func() CFDataClient {
					c := &fakes.FakeCfClient{}
					c.SetErroring(true)
					return c
				}(),
				watchedSet: func() *watch.Set {
					ws := watch.NewSet()
					ws.Add(pod.GroupVersionKind())

					return ws
				}(),
				tracker:          readiness.NewTracker(mgr.GetAPIReader(), false, false, false),
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder:  process.Get(),
			},
			expectedData:         map[fakes.CfDataKey]interface{}{},
			expectSyncMetric:     true,
			expectedMetricStatus: metrics.ErrorStatus,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cm.AddObject(context.Background(), &unstructured.Unstructured{Object: unstructuredPod})
			if tc.expectedMetricStatus == metrics.ActiveStatus {
				require.NoError(t, err)
			}

			assertExpecations(t, tc.cm, &unstructured.Unstructured{Object: unstructuredPod}, tc.expectedData, tc.expectSyncMetric, &tc.expectedMetricStatus)
		})
	}
}

func assertExpecations(t *testing.T, cm *CacheManager, instance *unstructured.Unstructured, expectedData map[fakes.CfDataKey]interface{}, expectSyncMetric bool, expectedMetricStatus *metrics.Status) {
	t.Helper()

	cfClient, ok := cm.cfClient.(*fakes.FakeCfClient)
	require.True(t, ok)

	require.True(t, cfClient.Contains(expectedData))

	syncKey := syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName())

	require.Equal(t, expectSyncMetric, cm.syncMetricsCache.HasObject(syncKey))

	if expectSyncMetric {
		require.Equal(t, *expectedMetricStatus, cm.syncMetricsCache.GetTags(syncKey).Status)
	}
}

// TestCacheManager_RemoveObject tests that we can remove objects from the cache.
func TestCacheManager_RemoveObject(t *testing.T) {
	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	mgr, _ := testutils.SetupManager(t, cfg)
	tracker := readiness.NewTracker(mgr.GetAPIReader(), false, false, false)
	makeDataClient := func() *fakes.FakeCfClient {
		c := &fakes.FakeCfClient{}
		_, err := c.AddData(context.Background(), &unstructured.Unstructured{Object: unstructuredPod})
		require.NoError(t, err)

		return c
	}

	tcs := []struct {
		name             string
		cm               *CacheManager
		expectSyncMetric bool
		expectedData     map[fakes.CfDataKey]interface{}
	}{
		{
			name: "RemoveObject happy path",
			cm: &CacheManager{
				cfClient: makeDataClient(),
				watchedSet: func() *watch.Set {
					ws := watch.NewSet()
					ws.Add(pod.GroupVersionKind())

					return ws
				}(),
				tracker:          tracker,
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder:  process.Get(),
			},
			expectedData:     map[fakes.CfDataKey]interface{}{},
			expectSyncMetric: false,
		},
		{
			name: "RemoveObject succeeds even if GVK is not watched",
			cm: &CacheManager{
				cfClient:         makeDataClient(),
				watchedSet:       watch.NewSet(),
				tracker:          tracker,
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder:  process.Get(),
			},
			expectedData:     map[fakes.CfDataKey]interface{}{},
			expectSyncMetric: false,
		},
		{
			name: "RemoveObject succeeds even if process excluded",
			cm: &CacheManager{
				cfClient: makeDataClient(),
				watchedSet: func() *watch.Set {
					ws := watch.NewSet()
					ws.Add(pod.GroupVersionKind())

					return ws
				}(),
				tracker:          tracker,
				syncMetricsCache: syncutil.NewMetricsCache(),
				processExcluder: func() *process.Excluder {
					processExcluder := process.New()
					processExcluder.Add([]configv1alpha1.MatchEntry{
						{
							ExcludedNamespaces: []wildcard.Wildcard{"test-ns"},
							Processes:          []string{"sync"},
						},
					})
					return processExcluder
				}(),
			},
			expectedData:     map[fakes.CfDataKey]interface{}{},
			expectSyncMetric: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.cm.RemoveObject(context.Background(), &unstructured.Unstructured{Object: unstructuredPod}))

			assertExpecations(t, tc.cm, &unstructured.Unstructured{Object: unstructuredPod}, tc.expectedData, tc.expectSyncMetric, nil)
		})
	}
}

// TestCacheManager_UpsertSource tests that we can modify the gvk aggregator and watched set when adding a new source.
func TestCacheManager_UpsertSource(t *testing.T) {
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	sourceA := aggregator.Key{Source: "a", ID: "source"}
	sourceB := aggregator.Key{Source: "b", ID: "source"}

	type sourcesAndGvk struct {
		source aggregator.Key
		gvks   []schema.GroupVersionKind
	}

	tcs := []struct {
		name           string
		sourcesAndGvks []sourcesAndGvk
		expectedGVKs   []schema.GroupVersionKind
	}{
		{
			name: "add one source",
			sourcesAndGvks: []sourcesAndGvk{
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{configMapGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK},
		},
		{
			name: "overwrite source",
			sourcesAndGvks: []sourcesAndGvk{
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{configMapGVK},
				},
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove source by not specifying any gvk",
			sourcesAndGvks: []sourcesAndGvk{
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{configMapGVK},
				},
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{},
		},
		{
			name: "add two disjoing sources",
			sourcesAndGvks: []sourcesAndGvk{
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{configMapGVK},
				},
				{
					source: sourceB,
					gvks:   []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
		{
			name: "add two sources with overlapping gvks",
			sourcesAndGvks: []sourcesAndGvk{
				{
					source: sourceA,
					gvks:   []schema.GroupVersionKind{configMapGVK, podGVK},
				},
				{
					source: sourceB,
					gvks:   []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cacheManager, ctx := makeCacheManager(t)

			for _, sourceAndGVK := range tc.sourcesAndGvks {
				require.NoError(t, cacheManager.UpsertSource(ctx, sourceAndGVK.source, sourceAndGVK.gvks))
			}

			require.ElementsMatch(t, cacheManager.watchedSet.Items(), tc.expectedGVKs)
			require.ElementsMatch(t, cacheManager.gvksToSync.GVKs(), tc.expectedGVKs)
		})
	}
}

// TestCacheManager_RemoveSource tests that we can modify the gvk aggregator when removing a source.
func TestCacheManager_RemoveSource(t *testing.T) {
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	sourceA := aggregator.Key{Source: "a", ID: "source"}
	sourceB := aggregator.Key{Source: "b", ID: "source"}

	tcs := []struct {
		name            string
		seed            func(c *CacheManager)
		sourcesToRemove []aggregator.Key
		expectedGVKs    []schema.GroupVersionKind
	}{
		{
			name: "remove disjoint source",
			seed: func(c *CacheManager) {
				require.NoError(t, c.gvksToSync.Upsert(sourceA, []schema.GroupVersionKind{podGVK}))
				require.NoError(t, c.gvksToSync.Upsert(sourceB, []schema.GroupVersionKind{configMapGVK}))
			},
			sourcesToRemove: []aggregator.Key{sourceB},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove overlapping source",
			seed: func(c *CacheManager) {
				require.NoError(t, c.gvksToSync.Upsert(sourceA, []schema.GroupVersionKind{podGVK}))
				require.NoError(t, c.gvksToSync.Upsert(sourceB, []schema.GroupVersionKind{podGVK}))
			},
			sourcesToRemove: []aggregator.Key{sourceB},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove non existing source",
			seed: func(c *CacheManager) {
				require.NoError(t, c.gvksToSync.Upsert(sourceA, []schema.GroupVersionKind{podGVK}))
			},
			sourcesToRemove: []aggregator.Key{sourceB},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cm, ctx := makeCacheManager(t)
			tc.seed(cm)

			for _, source := range tc.sourcesToRemove {
				require.NoError(t, cm.RemoveSource(ctx, source))
			}

			require.ElementsMatch(t, cm.gvksToSync.GVKs(), tc.expectedGVKs)
		})
	}
	cacheManager, ctx := makeCacheManager(t)

	// seed the gvk aggregator
	require.NoError(t, cacheManager.gvksToSync.Upsert(sourceA, []schema.GroupVersionKind{podGVK}))
	require.NoError(t, cacheManager.gvksToSync.Upsert(sourceB, []schema.GroupVersionKind{podGVK, configMapGVK}))

	// removing a source that is not the only one referencing a gvk ...
	require.NoError(t, cacheManager.RemoveSource(ctx, sourceB))
	// ... should not remove any gvks that are still referenced by other sources
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))
	require.False(t, cacheManager.gvksToSync.IsPresent(configMapGVK))

	require.NoError(t, cacheManager.RemoveSource(ctx, sourceA))
	require.False(t, cacheManager.gvksToSync.IsPresent(podGVK))
}

func unstructuredFor(gvk schema.GroupVersionKind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	u.SetNamespace("default")
	if gvk.Kind == "Pod" {
		u.Object["spec"] = map[string]interface{}{
			"containers": []map[string]interface{}{
				{
					"name":  "foo-container",
					"image": "foo-image",
				},
			},
		}
	}
	return u
}
