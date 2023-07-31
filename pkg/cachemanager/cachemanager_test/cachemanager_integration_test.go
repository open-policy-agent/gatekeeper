package cachemanager_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	eventuallyTimeout = 10 * time.Second
	eventuallyTicker  = 2 * time.Second
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

// TestCacheManager_replay_retries tests that we can retry GVKs that error out in the reply goroutine.
func TestCacheManager_replay_retries(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	failPlease := make(chan string, 2)
	reader := fakes.HookReader{
		Reader: c,
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			// Return an error the first go-around.
			var failKind string
			select {
			case failKind = <-failPlease:
			default:
			}
			if failKind != "" && list.GetObjectKind().GroupVersionKind().Kind == failKind {
				return fmt.Errorf("synthetic failure")
			}
			return c.List(ctx, list, opts...)
		},
	}

	cacheManager, dataStore, _, ctx := cacheManagerForTest(t, mgr, wm, reader)

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	// seed one gvk
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	// set up a scenario where the list from replay will fail a few times
	failPlease <- "ConfigMapList"
	failPlease <- "ConfigMapList"

	// this call should schedule a cache wipe and a replay for the configMapGVK
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected2 := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected2), eventuallyTimeout, eventuallyTicker)

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "creating ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, pod), "creating ConfigMap pod-1")
}

// TestCacheManager_AddSourceRemoveSource makes sure that we can add and remove multiple sources
// and changes to the underlying cache are reflected.
func TestCacheManager_AddSourceRemoveSource(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())
	cacheManager, dataStore, agg, ctx := cacheManagerForTest(t, mgr, wm, c)

	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	// Create configMaps to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	require.NoError(t, c.Create(ctx, cm2), "creating ConfigMap config-test-2")

	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	// now assert that the gvkAggregator looks as expected
	agg.IsPresent(configMapGVK)
	gvks := agg.List(syncSourceOne)
	require.Len(t, gvks, 2)
	_, foundConfigMap := gvks[configMapGVK]
	require.True(t, foundConfigMap)
	_, foundPod := gvks[podGVK]
	require.True(t, foundPod)

	// now remove the podgvk and make sure we don't have pods in the cache anymore
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected = map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)
	// now assert that the gvkAggregator looks as expected
	agg.IsPresent(configMapGVK)
	gvks = agg.List(syncSourceOne)
	require.Len(t, gvks, 1)
	_, foundConfigMap = gvks[configMapGVK]
	require.True(t, foundConfigMap)
	_, foundPod = gvks[podGVK]
	require.False(t, foundPod)

	// now make sure that adding another sync source with the same gvk has no side effects
	syncSourceTwo := aggregator.Key{Source: "source_b", ID: "ID_b"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))
	agg.IsPresent(configMapGVK)
	gvks = agg.List(syncSourceTwo)
	require.Len(t, gvks, 1)
	_, foundConfigMap = gvks[configMapGVK]
	require.True(t, foundConfigMap)

	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
	expected2 := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected2), eventuallyTimeout, eventuallyTicker)

	// now go on and unreference sourceTwo's gvks; this should schedule the config maps to be removed
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{}))
	expected3 := map[cachemanager.CfDataKey]interface{}{
		// config maps no longer required by any sync source
		// {Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		// {Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}: nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected3), eventuallyTimeout, eventuallyTicker)

	// now remove all the sources
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))

	// and expect an empty cache and empty aggregator
	require.Eventually(t, expectedCheck(cfClient, map[cachemanager.CfDataKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	require.True(t, len(agg.GVKs()) == 0)

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, cm2), "deleting ConfigMap config-test-2")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
}

// TestCacheManager_ExcludeProcesses makes sure that changing the process excluder
// in the cache manager triggers a re-evaluation of GVKs.
func TestCacheManager_ExcludeProcesses(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())
	cacheManager, dataStore, agg, ctx := cacheManagerForTest(t, mgr, wm, c)

	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
	}

	syncSource := aggregator.Key{Source: "source_b", ID: "ID_b"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSource, []schema.GroupVersionKind{configMapGVK}))
	// check that everything is correctly added at first
	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	// make sure that replacing w same process excluder is a no op
	sameExcluder := process.New()
	sameExcluder.Add([]configv1alpha1.MatchEntry{
		// same excluder as the one in makeCacheManagerForTest
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"sync"},
		},
	})
	cacheManager.ExcludeProcesses(sameExcluder)

	// now process exclude the remaining gvk, it should get removed by the background process.
	excluder := process.New()
	excluder.Add([]configv1alpha1.MatchEntry{
		// exclude the "default" namespace
		{
			ExcludedNamespaces: []wildcard.Wildcard{"default"},
			Processes:          []string{"sync"},
		},
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"sync"},
		},
	})
	cacheManager.ExcludeProcesses(excluder)

	require.Eventually(t, expectedCheck(cfClient, map[cachemanager.CfDataKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	// make sure the gvk is still in gvkAggregator
	require.True(t, len(agg.GVKs()) == 1)
	require.True(t, agg.IsPresent(configMapGVK))

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
}

func expectedCheck(cfClient *cachemanager.FakeCfClient, expected map[cachemanager.CfDataKey]interface{}) func() bool {
	return func() bool {
		if cfClient.Len() != len(expected) {
			return false
		}
		if cfClient.Contains(expected) {
			return true
		}

		return false
	}
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

func cacheManagerForTest(t *testing.T, mgr manager.Manager, wm *watch.Manager, reader client.Reader) (*cachemanager.CacheManager, cachemanager.CFDataClient, *aggregator.GVKAgreggator, context.Context) {
	ctx, cancelFunc := context.WithCancel(context.Background())

	cfClient := &cachemanager.FakeCfClient{}
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
	w, err := wm.NewRegistrar(
		"test-cache-manager",
		events)
	require.NoError(t, err)

	aggregator := aggregator.NewGVKAggregator()
	cfg := &cachemanager.Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watch.NewSet(),
		Registrar:        w,
		Reader:           reader,
		GVKAggregator:    aggregator,
	}
	cacheManager, err := cachemanager.NewCacheManager(cfg)
	require.NoError(t, err)

	syncAdder := sync.Adder{
		Events:       events,
		CacheManager: cacheManager,
	}
	require.NoError(t, syncAdder.Add(mgr), "registering sync controller")
	go func() {
		require.NoError(t, cacheManager.Start(ctx))
	}()

	t.Cleanup(func() {
		ctx.Done()
	})

	testutils.StartManager(ctx, t, mgr)

	t.Cleanup(func() {
		cancelFunc()
	})
	return cacheManager, cfClient, aggregator, ctx
}
