package cachemanager

import (
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 2)
}

func unitCacheManagerForTest(t *testing.T) (*CacheManager, context.Context) {
	cm, _, ctx := makeCacheManager(t)
	return cm, ctx
}

func makeCacheManager(t *testing.T) (*CacheManager, client.Client, context.Context) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx, cancelFunc := context.WithCancel(context.Background())

	cfClient := &FakeCfClient{}
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

	cacheManager, err := NewCacheManager(&Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watch.NewSet(),
		Registrar:        w,
		Reader:           c,
		GVKAggregator:    aggregator.NewGVKAggregator(),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx.Done()
	})

	t.Cleanup(func() {
		cancelFunc()
	})

	testutils.StartManager(ctx, t, mgr)

	return cacheManager, c, ctx
}

// TestCacheManager_wipeCacheIfNeeded.
func TestCacheManager_wipeCacheIfNeeded(t *testing.T) {
	cacheManager, ctx := unitCacheManagerForTest(t)
	cfClient, ok := cacheManager.cfClient.(*FakeCfClient)
	require.True(t, ok)

	// seed one gvk
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	_, err := cfClient.AddData(ctx, cm)
	require.NoError(t, err, "adding ConfigMap config-test-1 in cfClient")

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	require.NoError(t, cacheManager.gvksToSync.Upsert(aggregator.Key{Source: "foo", ID: "bar"}, []schema.GroupVersionKind{podGVK}))

	cacheManager.gvksToDeleteFromCache.Add(configMapGVK)
	cacheManager.wipeCacheIfNeeded(ctx)

	require.False(t, cfClient.HasGVK(configMapGVK))
	require.ElementsMatch(t, cacheManager.gvksToSync.GVKs(), []schema.GroupVersionKind{podGVK})
}

// TestCacheManager_syncGVKInstances tests that GVK instances can be listed and added to the cfClient client.
func TestCacheManager_syncGVKInstances(t *testing.T) {
	cacheManager, c, ctx := makeCacheManager(t)

	configMapGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
	// Create configMaps to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")
	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	require.NoError(t, c.Create(ctx, cm2), "creating ConfigMap config-test-2")

	cacheManager.watchedSet.Add(configMapGVK)
	require.NoError(t, cacheManager.syncGVK(ctx, configMapGVK))

	cfClient, ok := cacheManager.cfClient.(*FakeCfClient)
	require.True(t, ok)
	expected := map[CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
	}

	require.Equal(t, 2, cfClient.Len())
	require.True(t, cfClient.Contains(expected))

	// wipe cache
	require.NoError(t, cacheManager.wipeData(ctx))
	require.False(t, cfClient.Contains(expected))

	// create a second GVK
	podGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	// Create pods to test for
	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	pod2 := unstructuredFor(podGVK, "pod-2")
	require.NoError(t, c.Create(ctx, pod2), "creating Pod pod-2")

	pod3 := unstructuredFor(podGVK, "pod-3")
	require.NoError(t, c.Create(ctx, pod3), "creating Pod pod-3")

	cacheManager.watchedSet.Add(podGVK)
	require.NoError(t, cacheManager.syncGVK(ctx, configMapGVK))
	require.NoError(t, cacheManager.syncGVK(ctx, podGVK))

	expected = map[CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
		{Gvk: podGVK, Key: "default/pod-2"}:               nil,
		{Gvk: podGVK, Key: "default/pod-3"}:               nil,
	}

	require.Equal(t, 5, cfClient.Len())
	require.True(t, cfClient.Contains(expected))

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, cm2), "deleting ConfigMap config-test-2")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
	require.NoError(t, c.Delete(ctx, pod3), "deleting Pod pod-3")
	require.NoError(t, c.Delete(ctx, pod2), "deleting Pod pod-2")
}

// TestCacheManager_wipeCacheIfNeeded_excluderChanges tests that we can remove gvks that were not previously process excluded but are now.
func TestCacheManager_wipeCacheIfNeeded_excluderChanges(t *testing.T) {
	cacheManager, ctx := unitCacheManagerForTest(t)
	cfClient, ok := cacheManager.cfClient.(*FakeCfClient)
	require.True(t, ok)

	// seed gvks
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("excluded-ns")
	_, err := cfClient.AddData(ctx, cm)
	require.NoError(t, err, "adding ConfigMap config-test-1 in cfClient")
	cacheManager.watchedSet.Add(configMapGVK)

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := unstructuredFor(configMapGVK, "pod-test-1")
	pod.SetNamespace("excluded-ns")
	_, err = cfClient.AddData(ctx, pod)
	require.NoError(t, err, "adding Pod pod-test-1 in cfClient")
	cacheManager.watchedSet.Add(podGVK)

	cacheManager.ExcludeProcesses(newSyncExcluderFor("excluded-ns"))
	cacheManager.wipeCacheIfNeeded(ctx)

	// the cache manager should not be watching any of the gvks that are now excluded
	require.False(t, cfClient.HasGVK(configMapGVK))
	require.False(t, cfClient.HasGVK(podGVK))
	require.False(t, cacheManager.excluderChanged)
}

// TestCacheManager_AddObject_RemoveObject tests that we can add/ remove objects in the cache.
func TestCacheManager_AddObject_RemoveObject(t *testing.T) {
	cm, ctx := unitCacheManagerForTest(t)

	cfClient, ok := cm.cfClient.(*FakeCfClient)
	require.True(t, ok)

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	// when gvk is watched, we expect Add, Remove to work
	cm.watchedSet.Add(pod.GroupVersionKind())

	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.True(t, cfClient.HasGVK(pod.GroupVersionKind()))

	// now remove the object and verify it's removed
	require.NoError(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, cfClient.HasGVK(pod.GroupVersionKind()))

	cm.watchedSet.Remove(pod.GroupVersionKind())
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, cfClient.HasGVK(pod.GroupVersionKind())) // we drop calls for gvks that are not watched
}

// TestCacheManager_AddObject_processExclusion makes sure that we don't add objects that are process excluded.
func TestCacheManager_AddObject_processExclusion(t *testing.T) {
	cm, ctx := unitCacheManagerForTest(t)
	processExcluder := process.Get()
	processExcluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"test-ns-excluded"},
			Processes:          []string{"sync"},
		},
	})
	cm.processExcluder.Replace(processExcluder)

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns-excluded"),
		fakes.WithName("test-name"),
	)
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod from excluded namespace is not added to the cache
	cfClient, ok := cm.cfClient.(*FakeCfClient)
	require.True(t, ok)
	require.False(t, cfClient.HasGVK(pod.GroupVersionKind()))
	require.False(t, cfClient.Contains(map[CfDataKey]interface{}{{Gvk: podGVK, Key: "default/config-test-1"}: nil}))
}

// TestCacheManager_cfClient_errors tests that the cache manager responds to errors from the cfClient client.
func TestCacheManager_cfClient_errors(t *testing.T) {
	cm, ctx := unitCacheManagerForTest(t)
	cfClient, ok := cm.cfClient.(*FakeCfClient)
	require.True(t, ok)
	cfClient.SetErroring(true) // This will cause AddObject, RemoveObject to err

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	cm.watchedSet.Add(pod.GroupVersionKind())

	// test that cm bubbles up the errors
	require.ErrorContains(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
	require.ErrorContains(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
}

// TestCacheManager_AddSource tests that we can modify the gvk aggregator and watched set when adding a new source.
func TestCacheManager_AddSource(t *testing.T) {
	cacheManager, ctx := unitCacheManagerForTest(t)
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	nsGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	sourceA := aggregator.Key{Source: "a", ID: "source"}
	sourceB := aggregator.Key{Source: "b", ID: "source"}

	// given two sources with overlapping gvks ...
	require.NoError(t, cacheManager.UpsertSource(ctx, sourceA, []schema.GroupVersionKind{podGVK}))
	require.NoError(t, cacheManager.UpsertSource(ctx, sourceB, []schema.GroupVersionKind{podGVK, configMapGVK}))

	// ... expect the aggregator to dedup
	require.True(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))
	require.ElementsMatch(t, cacheManager.watchedSet.Items(), []schema.GroupVersionKind{podGVK, configMapGVK})

	// adding a source without a previously added gvk ...
	require.NoError(t, cacheManager.UpsertSource(ctx, sourceB, []schema.GroupVersionKind{configMapGVK}))
	// ... should not remove any gvks that are still referenced by other sources
	require.True(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))

	// adding a source that modifies the only reference to a gvk ...
	require.NoError(t, cacheManager.UpsertSource(ctx, sourceB, []schema.GroupVersionKind{nsGVK}))

	// ... will effectively remove the gvk from the aggregator
	require.False(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(nsGVK))
}

// TestCacheManager_RemoveSource tests that we can modify the gvk aggregator when removing a source.
func TestCacheManager_RemoveSource(t *testing.T) {
	cacheManager, ctx := unitCacheManagerForTest(t)
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	sourceA := aggregator.Key{Source: "a", ID: "source"}
	sourceB := aggregator.Key{Source: "b", ID: "source"}

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

func newSyncExcluderFor(nsToExclude string) *process.Excluder {
	excluder := process.New()
	excluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{wildcard.Wildcard(nsToExclude)},
			Processes:          []string{"sync"},
		},
		// exclude kube-system by default to prevent noise
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"sync"},
		},
	})

	return excluder
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
