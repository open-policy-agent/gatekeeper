package cachemanager

import (
	"context"
	"errors"
	"fmt"
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

var (
	configMapGVK   = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	podGVK         = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
	nsGVK          = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	nonExistentGVK = schema.GroupVersionKind{Version: "v1", Kind: "DoesNotExist"}

	configKey   = aggregator.Key{Source: "config", ID: "config"}
	syncsetAKey = aggregator.Key{Source: "syncset", ID: "a"}
	syncsetBkey = aggregator.Key{Source: "syncset", ID: "b"}
)

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
	dataClientForTest := func() CFDataClient {
		cfdc := &fakes.FakeCfClient{}

		cm := fakes.UnstructuredFor(configMapGVK, "", "config-test-1")
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

type source struct {
	key  aggregator.Key
	gvks []schema.GroupVersionKind
}

// TestCacheManager_UpsertSource tests that we can modify the gvk aggregator and watched set when adding a new source.
func TestCacheManager_UpsertSource(t *testing.T) {
	tcs := []struct {
		name         string
		sources      []source
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "add one source",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK},
		},
		{
			name: "overwrite source",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK},
				},
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove GVK from a source",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK},
				},
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{},
		},
		{
			name: "add two disjoint sources",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK},
				},
				{
					key:  syncsetAKey,
					gvks: []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
		{
			name: "add two sources with fully overlapping gvks",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{podGVK},
				},
				{
					key:  syncsetAKey,
					gvks: []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{podGVK},
		},
		{
			name: "add two sources with partially overlapping gvks",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK, podGVK},
				},
				{
					key:  syncsetAKey,
					gvks: []schema.GroupVersionKind{podGVK},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cacheManager, ctx := makeCacheManager(t)

			for _, source := range tc.sources {
				require.NoError(t, cacheManager.UpsertSource(ctx, source.key, source.gvks), fmt.Sprintf("while upserting source: %s", source.key))
			}

			require.ElementsMatch(t, cacheManager.watchedSet.Items(), tc.expectedGVKs)
			require.ElementsMatch(t, cacheManager.gvksToSync.GVKs(), tc.expectedGVKs)
		})
	}
}

func TestCacheManager_UpsertSource_errorcases(t *testing.T) {
	type source struct {
		key     aggregator.Key
		gvks    []schema.GroupVersionKind
		wantErr bool
	}

	tcs := []struct {
		name         string
		sources      []source
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "add two sources where one fails to establish all watches",
			sources: []source{
				{
					key:  configKey,
					gvks: []schema.GroupVersionKind{configMapGVK},
				},
				{
					key:  syncsetAKey,
					gvks: []schema.GroupVersionKind{podGVK, nonExistentGVK},
					// UpsertSource will err out because of nonExistentGVK
					wantErr: true,
				},
				{
					key:  syncsetBkey,
					gvks: []schema.GroupVersionKind{nsGVK},
					// this call will not error out even though we previously added a non existent gvk to a different sync source.
					// this way the errors in watch manager caused by one sync source do not impact the other if they are not related
					// to the gvks it specifies.
				},
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK, nonExistentGVK, nsGVK},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cacheManager, ctx := makeCacheManager(t)

			for _, source := range tc.sources {
				if source.wantErr {
					require.Error(t, cacheManager.UpsertSource(ctx, source.key, source.gvks), fmt.Sprintf("while upserting source: %s", source.key))
				} else {
					require.NoError(t, cacheManager.UpsertSource(ctx, source.key, source.gvks), fmt.Sprintf("while upserting source: %s", source.key))
				}
			}

			require.ElementsMatch(t, cacheManager.watchedSet.Items(), tc.expectedGVKs)
			require.ElementsMatch(t, cacheManager.gvksToSync.GVKs(), tc.expectedGVKs)
		})
	}
}

// TestCacheManager_RemoveSource tests that we can modify the gvk aggregator when removing a source.
func TestCacheManager_RemoveSource(t *testing.T) {
	tcs := []struct {
		name            string
		existingSources []source
		sourcesToRemove []aggregator.Key
		expectedGVKs    []schema.GroupVersionKind
	}{
		{
			name: "remove disjoint source",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{podGVK}},
				{syncsetAKey, []schema.GroupVersionKind{configMapGVK}},
			},
			sourcesToRemove: []aggregator.Key{syncsetAKey},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove fully overlapping source",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{podGVK}},
				{syncsetAKey, []schema.GroupVersionKind{podGVK}},
			},
			sourcesToRemove: []aggregator.Key{syncsetAKey},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove partially overlapping source",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{podGVK}},
				{syncsetAKey, []schema.GroupVersionKind{podGVK, configMapGVK}},
			},
			sourcesToRemove: []aggregator.Key{configKey},
			expectedGVKs:    []schema.GroupVersionKind{podGVK, configMapGVK},
		},
		{
			name: "remove non existing source",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{podGVK}},
			},
			sourcesToRemove: []aggregator.Key{syncsetAKey},
			expectedGVKs:    []schema.GroupVersionKind{podGVK},
		},
		{
			name: "remove source with a non existing gvk",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{nonExistentGVK}},
			},
			sourcesToRemove: []aggregator.Key{configKey},
			expectedGVKs:    []schema.GroupVersionKind{},
		},
		{
			name: "remove source from a watch set with a non existing gvk",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{nonExistentGVK}},
				{syncsetAKey, []schema.GroupVersionKind{podGVK}},
			},
			sourcesToRemove: []aggregator.Key{syncsetAKey},
			expectedGVKs:    []schema.GroupVersionKind{nonExistentGVK},
		},
		{
			name: "remove source with non existent gvk from a watch set with a remaining non existing gvk",
			existingSources: []source{
				{configKey, []schema.GroupVersionKind{nonExistentGVK}},
				{syncsetAKey, []schema.GroupVersionKind{nonExistentGVK}},
			},
			sourcesToRemove: []aggregator.Key{syncsetAKey},
			expectedGVKs:    []schema.GroupVersionKind{nonExistentGVK},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			cm, ctx := makeCacheManager(t)
			// seed the cachemanager internals
			for _, s := range tc.existingSources {
				cm.gvksToSync.Upsert(s.key, s.gvks)
			}

			for _, source := range tc.sourcesToRemove {
				require.NoError(t, cm.RemoveSource(ctx, source))
			}

			require.ElementsMatch(t, cm.gvksToSync.GVKs(), tc.expectedGVKs)
		})
	}
}

func Test_interpretErr(t *testing.T) {
	gvk1 := schema.GroupVersionKind{Group: "g1", Version: "v1", Kind: "k1"}
	gvk2 := schema.GroupVersionKind{Group: "g2", Version: "v2", Kind: "k2"}
	someErr := errors.New("some err")
	gvk1Err := watch.NewErrorList()
	gvk1Err.AddGVKErr(gvk1, someErr)
	genErr := watch.NewErrorList()
	genErr.Err(someErr)
	genErr.AddGVKErr(gvk1, someErr)
	gvk2Err := watch.NewErrorList()
	gvk2Err.RemoveGVKErr(gvk2, someErr)

	cases := []struct {
		name                   string
		inputErr               error
		inputGVK               []schema.GroupVersionKind
		expectedAddGVKFailures []schema.GroupVersionKind
		expectGeneral          bool
	}{
		{
			name: "nil err",
		},
		{
			name:                   "intersection exists",
			inputErr:               fmt.Errorf("some err: %w", gvk1Err),
			inputGVK:               []schema.GroupVersionKind{gvk1},
			expectedAddGVKFailures: []schema.GroupVersionKind{gvk1},
		},
		{
			name:     "intersection does not exist",
			inputErr: gvk1Err,
			inputGVK: []schema.GroupVersionKind{gvk2},
		},
		{
			name:     "gvk watch failing to remove",
			inputErr: gvk2Err,
		},
		{
			name:          "non-watchmanager error reports general error with no GVKs",
			inputErr:      fmt.Errorf("some err: %w", someErr),
			inputGVK:      []schema.GroupVersionKind{gvk1},
			expectGeneral: true,
		},
		{
			name:          "general error with failing gvks too",
			inputErr:      fmt.Errorf("some err: %w", genErr),
			inputGVK:      []schema.GroupVersionKind{gvk1, gvk2},
			expectGeneral: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			general, addGVKs := interpretErr(tc.inputErr, tc.inputGVK)

			require.Equal(t, tc.expectGeneral, general)
			require.ElementsMatch(t, addGVKs, tc.expectedAddGVKFailures)
		})
	}
}

func Test_handleDanglingWatches(t *testing.T) {
	gvk1 := schema.GroupVersionKind{Group: "g1", Version: "v1", Kind: "k1"}
	gvk2 := schema.GroupVersionKind{Group: "g2", Version: "v2", Kind: "k2"}

	cases := []struct {
		name              string
		alreadyDangling   *watch.Set
		removeGVKFailures []schema.GroupVersionKind
		expectedDangling  *watch.Set
	}{
		{
			name:             "no watches dangling, nothing to remove",
			expectedDangling: watch.NewSet(),
		},
		{
			name:             "no watches dangling, something to remove",
			expectedDangling: watch.NewSet(),
		},
		{
			name:              "watches dangling, finally removed",
			alreadyDangling:   watch.SetFrom([]schema.GroupVersionKind{gvk1}),
			removeGVKFailures: []schema.GroupVersionKind{},
			expectedDangling:  watch.SetFrom([]schema.GroupVersionKind{}),
		},
		{
			name:              "watches dangling, keep dangling",
			alreadyDangling:   watch.SetFrom([]schema.GroupVersionKind{gvk1}),
			removeGVKFailures: []schema.GroupVersionKind{gvk1},
			expectedDangling:  watch.SetFrom([]schema.GroupVersionKind{gvk1}),
		},
		{
			name:              "watches dangling, some keep dangling",
			alreadyDangling:   watch.SetFrom([]schema.GroupVersionKind{gvk2, gvk1}),
			removeGVKFailures: []schema.GroupVersionKind{gvk1},
			expectedDangling:  watch.SetFrom([]schema.GroupVersionKind{gvk1}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm, _ := makeCacheManager(t)
			if tc.alreadyDangling != nil {
				cm.danglingWatches.AddSet(tc.alreadyDangling)
			}

			cm.handleDanglingWatches(tc.removeGVKFailures)

			if tc.expectedDangling != nil {
				require.ElementsMatch(t, tc.expectedDangling.Items(), cm.danglingWatches.Items())
			} else {
				require.Empty(t, cm.danglingWatches)
			}
		})
	}
}
