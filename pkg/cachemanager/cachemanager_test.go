package cachemanager

import (
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestCacheManager_wipeCacheIfNeeded.
func TestCacheManager_wipeCacheIfNeeded(t *testing.T) {
	cacheManager, ctx := makeUnitCacheManagerForTest(t)
	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	// seed one gvk
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	_, err := opaClient.AddData(ctx, cm)
	require.NoError(t, err, "adding ConfigMap config-test-1 in opa")

	// prep gvkAggregator for updates to be picked up in makeUpdates
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	require.NoError(t, cacheManager.gvksToSync.Upsert(aggregator.Key{Source: "foo", ID: "bar"}, []schema.GroupVersionKind{podGVK}))

	cacheManager.gvksToDeleteFromCache.Add(configMapGVK)
	cacheManager.wipeCacheIfNeeded(ctx)

	require.False(t, opaClient.HasGVK(configMapGVK))
	require.ElementsMatch(t, cacheManager.gvksToSync.GVKs(), []schema.GroupVersionKind{podGVK})
}

// TestCacheManager_wipeCacheIfNeeded_excluderChanges tests that we can remove gvks that were not previously process excluded but are now.
func TestCacheManager_wipeCacheIfNeeded_excluderChanges(t *testing.T) {
	cacheManager, ctx := makeUnitCacheManagerForTest(t)
	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	// seed gvks
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("excluded-ns")
	_, err := opaClient.AddData(ctx, cm)
	require.NoError(t, err, "adding ConfigMap config-test-1 in opa")
	cacheManager.watchedSet.Add(configMapGVK)

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := unstructuredFor(configMapGVK, "pod-test-1")
	pod.SetNamespace("excluded-ns")
	_, err = opaClient.AddData(ctx, pod)
	require.NoError(t, err, "adding Pod pod-test-1 in opa")
	cacheManager.watchedSet.Add(podGVK)

	cacheManager.ExcludeProcesses(newSyncExcluderFor("excluded-ns"))
	cacheManager.wipeCacheIfNeeded(ctx)

	// the cache manager should not be watching any of the gvks that are now excluded
	require.False(t, opaClient.HasGVK(configMapGVK))
	require.False(t, opaClient.HasGVK(podGVK))
	require.False(t, cacheManager.excluderChanged)
}

// TestCacheManager_AddObject_RemoveObject tests that we can add/ remove objects in the cache.
func TestCacheManager_AddObject_RemoveObject(t *testing.T) {
	cm, ctx := makeUnitCacheManagerForTest(t)

	opaClient, ok := cm.opa.(*fakes.FakeOpa)
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
	require.True(t, opaClient.HasGVK(pod.GroupVersionKind()))

	// now remove the object and verify it's removed
	require.NoError(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))

	cm.watchedSet.Remove(pod.GroupVersionKind())
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind())) // we drop calls for gvks that are not watched
}

// TestCacheManager_AddObject_processExclusion makes sure that we don't add objects that are process excluded.
func TestCacheManager_AddObject_processExclusion(t *testing.T) {
	cm, ctx := makeUnitCacheManagerForTest(t)
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

	// test that pod from excluded namespace is not cache managed
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
	require.False(t, opaClient.Contains(map[fakes.OpaKey]interface{}{{Gvk: podGVK, Key: "default/config-test-1"}: nil}))
}

// TestCacheManager_opaClient_errors tests that the cache manager responds to errors from the opa client.
func TestCacheManager_opaClient_errors(t *testing.T) {
	cm, ctx := makeUnitCacheManagerForTest(t)
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	opaClient.SetErroring(true) // This will cause AddObject, RemoveObject to err

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
	cacheManager, _, ctx := makeCacheManagerForTest(t, false, true)
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	nsGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	sourceA := aggregator.Key{Source: "a", ID: "source"}
	sourceB := aggregator.Key{Source: "b", ID: "source"}

	// given two sources with overlapping gvks ...
	require.NoError(t, cacheManager.AddSource(ctx, sourceA, []schema.GroupVersionKind{podGVK}))
	require.NoError(t, cacheManager.AddSource(ctx, sourceB, []schema.GroupVersionKind{podGVK, configMapGVK}))

	// ... expect the aggregator to dedup
	require.True(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))
	require.ElementsMatch(t, cacheManager.watchedSet.Items(), []schema.GroupVersionKind{podGVK, configMapGVK})

	// adding a source without a previously added gvk ...
	require.NoError(t, cacheManager.AddSource(ctx, sourceB, []schema.GroupVersionKind{configMapGVK}))
	// ... should not remove any gvks that are still referenced by other sources
	require.True(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))

	// adding a source that modifies the only reference to a gvk ...
	require.NoError(t, cacheManager.AddSource(ctx, sourceB, []schema.GroupVersionKind{nsGVK}))

	// ... will effectively remove the gvk from the aggregator
	require.False(t, cacheManager.gvksToSync.IsPresent(configMapGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(podGVK))
	require.True(t, cacheManager.gvksToSync.IsPresent(nsGVK))
}

// TestCacheManager_RemoveSource tests that we can modify the gvk aggregator when removing a source.
func TestCacheManager_RemoveSource(t *testing.T) {
	cacheManager, _, ctx := makeCacheManagerForTest(t, false, true)
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
