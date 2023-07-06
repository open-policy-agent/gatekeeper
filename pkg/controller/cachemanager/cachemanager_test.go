package cachemanager

import (
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil/aggregator"
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
	testutils.StartControlPlane(m, &cfg, 3)
}

// TestCacheManager_AddObject_RemoveObject tests that we can add/ remove objects in the cache.
func TestCacheManager_AddObject_RemoveObject(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod is cache managed
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	require.True(t, opaClient.HasGVK(pod.GroupVersionKind()))

	// now remove the object and verify it's removed
	require.NoError(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_processExclusion makes sure that we don't add objects that are process excluded.
func TestCacheManager_processExclusion(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)
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
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod from excluded namespace is not cache managed
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_errors tests that the cache manager responds to errors from the opa client.
func TestCacheManager_errors(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	opaClient.SetErroring(true) // This will cause AddObject, RemoveObject to err

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	// test that cm bubbles up the errors
	require.ErrorContains(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
	require.ErrorContains(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
}

// TestCacheManager_listAndSyncData tests that the cache manager can add gvks to the data store.
func TestCacheManager_listAndSyncData(t *testing.T) {
	cacheManager, c, ctx := makeCacheManagerForTest(t, false, false)

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

	require.NoError(t, cacheManager.listAndSyncDataForGVK(ctx, configMapGVK))

	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	expected := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
	}

	require.Equal(t, 2, opaClient.Len())
	require.True(t, opaClient.Contains(expected))

	// wipe cache
	require.NoError(t, cacheManager.wipeData(ctx))
	require.False(t, opaClient.Contains(expected))

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

	syncedSet := cacheManager.listAndSyncData(ctx, []schema.GroupVersionKind{configMapGVK, podGVK}, c)
	require.ElementsMatch(t, syncedSet.Items(), []schema.GroupVersionKind{configMapGVK, podGVK})

	expected = map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
		{Gvk: podGVK, Key: "default/pod-2"}:               nil,
		{Gvk: podGVK, Key: "default/pod-3"}:               nil,
	}

	require.Equal(t, 5, opaClient.Len())
	require.True(t, opaClient.Contains(expected))

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, cm2), "deleting ConfigMap config-test-2")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
	require.NoError(t, c.Delete(ctx, pod3), "deleting Pod pod-3")
	require.NoError(t, c.Delete(ctx, pod2), "deleting Pod pod-2")
}

// TestCacheManager_makeUpdates tests that we can remove and add gvks to the data store.
func TestCacheManager_makeUpdates(t *testing.T) {
	cacheManager, c, ctx := makeCacheManagerForTest(t, false, false)
	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	// seed one gvk
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	_, err := opaClient.AddData(ctx, cm)
	require.NoError(t, err, "creating ConfigMap config-test-1 in opa")

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	// prep gvkAggregator for updates to be picked up in makeUpdates
	require.NoError(t, cacheManager.gvkAggregator.Upsert(aggregator.Key{Source: "foo", ID: "bar"}, []schema.GroupVersionKind{podGVK}))

	gvksInAgg := watch.NewSet()
	gvksInAgg.Add(cacheManager.gvkAggregator.ListAllGVKs()...)
	cacheManager.makeUpdatesForSpecInTime(ctx, gvksInAgg, false)

	// expect the following instances to be in the data store
	expected := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}
	require.Equal(t, 2, opaClient.Len())
	require.True(t, opaClient.Contains(expected))

	// cleanup
	// cm was not actually created thru the client, so no need for this.
	// require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
}

// TestCacheManager_AddSourceRemoveSource makes sure that we can add and remove multiple sources
// and changes to the underlying cache are reflected.
func TestCacheManager_AddSourceRemoveSource(t *testing.T) {
	cacheManager, c, ctx := makeCacheManagerForTest(t, true, true)

	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	// Create configMaps to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	require.NoError(t, c.Create(ctx, cm2), "creating ConfigMap config-test-2")

	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(opaClient, expected), testutils.EventuallyTimeout, testutils.EventuallyTicker)

	// now assert that the gvkAggregator looks as expected
	cacheManager.gvkAggregator.IsPresent(configMapGVK)
	gvks := cacheManager.gvkAggregator.List(syncSourceOne)
	require.Len(t, gvks, 2)
	_, foundConfigMap := gvks[configMapGVK]
	require.True(t, foundConfigMap)
	_, foundPod := gvks[podGVK]
	require.True(t, foundPod)

	// now remove the podgvk and make sure we don't have pods in the cache anymore
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected = map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected), testutils.EventuallyTimeout, testutils.EventuallyTicker)
	// now assert that the gvkAggregator looks as expected
	cacheManager.gvkAggregator.IsPresent(configMapGVK)
	gvks = cacheManager.gvkAggregator.List(syncSourceOne)
	require.Len(t, gvks, 1)
	_, foundConfigMap = gvks[configMapGVK]
	require.True(t, foundConfigMap)
	_, foundPod = gvks[podGVK]
	require.False(t, foundPod)

	// now make sure that adding another sync source with the same gvk has no side effects
	syncSourceTwo := aggregator.Key{Source: "source_b", ID: "ID_b"}
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))

	reqConditionForAgg := func() bool {
		cacheManager.gvkAggregator.IsPresent(configMapGVK)
		gvks := cacheManager.gvkAggregator.List(syncSourceOne)
		if len(gvks) != 1 {
			return false
		}
		_, found := gvks[configMapGVK]
		if !found {
			return false
		}

		gvks2 := cacheManager.gvkAggregator.List(syncSourceTwo)
		if len(gvks2) != 1 {
			return false
		}
		_, found2 := gvks2[configMapGVK]
		return found2
	}
	require.Eventually(t, reqConditionForAgg, testutils.EventuallyTimeout, testutils.EventuallyTicker)

	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
	expected2 := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected2), testutils.EventuallyTimeout, testutils.EventuallyTicker)

	// now go on and unreference sourceTwo's gvks; this should schedule the config maps to be removed
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceTwo, []schema.GroupVersionKind{}))
	expected3 := map[fakes.OpaKey]interface{}{
		// config maps no longer required by any sync source
		// {Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		// {Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}: nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected3), testutils.EventuallyTimeout, testutils.EventuallyTicker)

	// now remove all the sources
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))

	// and expect an empty cache and empty aggregator
	require.Eventually(t, expectedCheck(opaClient, map[fakes.OpaKey]interface{}{}), testutils.EventuallyTimeout, testutils.EventuallyTicker)
	require.True(t, len(cacheManager.gvkAggregator.ListAllGVKs()) == 0)

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, cm2), "deleting ConfigMap config-test-2")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
}

// TestCacheManager_ExcludeProcesses makes sure that changing the process excluder
// in the cache manager triggers a re-evaluation of GVKs.
func TestCacheManager_ExcludeProcesses(t *testing.T) {
	cacheManager, c, ctx := makeCacheManagerForTest(t, true, true)

	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	expected := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
	}

	syncSource := aggregator.Key{Source: "source_b", ID: "ID_b"}
	require.NoError(t, cacheManager.AddSource(ctx, syncSource, []schema.GroupVersionKind{configMapGVK}))
	// check that everything is well added at first
	require.Eventually(t, expectedCheck(opaClient, expected), testutils.EventuallyTimeout, testutils.EventuallyTicker)

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
	require.True(t, cacheManager.processExcluder.Equals(sameExcluder))

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

	require.Eventually(t, expectedCheck(opaClient, map[fakes.OpaKey]interface{}{}), testutils.EventuallyTimeout, testutils.EventuallyTicker)
	// make sure the gvk is still in gvkAggregator
	require.True(t, len(cacheManager.gvkAggregator.ListAllGVKs()) == 1)
	require.True(t, cacheManager.gvkAggregator.IsPresent(configMapGVK))

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
}

func expectedCheck(opaClient *fakes.FakeOpa, expected map[fakes.OpaKey]interface{}) func() bool {
	return func() bool {
		if opaClient.Len() != len(expected) {
			return false
		}
		if opaClient.Contains(expected) {
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

func makeCacheManagerForTest(t *testing.T, startCache, startManager bool) (*CacheManager, client.Client, context.Context) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	mgr, wm := testutils.SetupManager(t, cfg)

	c := testclient.NewRetryClient(mgr.GetClient())
	opaClient := &fakes.FakeOpa{}
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
		Opa:              opaClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watch.NewSet(),
		Registrar:        w,
		Reader:           c,
	})
	require.NoError(t, err)

	if startCache {
		go func() {
			require.NoError(t, cacheManager.Start(ctx))
		}()

		t.Cleanup(func() {
			ctx.Done()
		})
	}

	if startManager {
		testutils.StartManager(ctx, t, mgr)
	}

	t.Cleanup(func() {
		cancelFunc()
	})
	return cacheManager, c, ctx
}
