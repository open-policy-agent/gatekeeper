package cachemanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	eventuallyTimeout = 10 * time.Second
	eventuallyTicker  = 2 * time.Second
)

// TestCacheManager_replay_retries tests that we can retry GVKs that error out in the reply goroutine.
func TestCacheManager_replay_retries(t *testing.T) {
	cacheManager, c, ctx := makeCacheManagerForTest(t, true, true)

	failPlease := make(chan string, 2)
	cacheManager.reader = fakes.HookReader{
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

	opaClient, ok := cacheManager.opa.(*fakes.FakeOpa)
	require.True(t, ok)

	// seed one gvk
	configMapGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	cm := unstructuredFor(configMapGVK, "config-test-1")
	require.NoError(t, c.Create(ctx, cm), "creating ConfigMap config-test-1")

	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := unstructuredFor(podGVK, "pod-1")
	require.NoError(t, c.Create(ctx, pod), "creating Pod pod-1")

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(opaClient, expected), eventuallyTimeout, eventuallyTicker)

	// set up a scenario where the list from replay will fail a few times
	failPlease <- "ConfigMapList"
	failPlease <- "ConfigMapList"

	// this call should make schedule a cache wipe and a replay for the configMapGVK
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected2 := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected2), eventuallyTimeout, eventuallyTicker)

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "creating ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, pod), "creating ConfigMap pod-1")
}

// TestCacheManager_syncGVKInstances tests that GVK instances can be listed and added to the opa client.
func TestCacheManager_syncGVKInstances(t *testing.T) {
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

	cacheManager.watchedSet.Add(configMapGVK)
	require.NoError(t, cacheManager.syncGVK(ctx, configMapGVK))

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

	cacheManager.watchedSet.Add(podGVK)
	require.NoError(t, cacheManager.syncGVK(ctx, configMapGVK))
	require.NoError(t, cacheManager.syncGVK(ctx, podGVK))

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

	require.Eventually(t, expectedCheck(opaClient, expected), eventuallyTimeout, eventuallyTicker)

	// now assert that the gvkAggregator looks as expected
	cacheManager.gvksToSync.IsPresent(configMapGVK)
	gvks := cacheManager.gvksToSync.List(syncSourceOne)
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
	require.Eventually(t, expectedCheck(opaClient, expected), eventuallyTimeout, eventuallyTicker)
	// now assert that the gvkAggregator looks as expected
	cacheManager.gvksToSync.IsPresent(configMapGVK)
	gvks = cacheManager.gvksToSync.List(syncSourceOne)
	require.Len(t, gvks, 1)
	_, foundConfigMap = gvks[configMapGVK]
	require.True(t, foundConfigMap)
	_, foundPod = gvks[podGVK]
	require.False(t, foundPod)

	// now make sure that adding another sync source with the same gvk has no side effects
	syncSourceTwo := aggregator.Key{Source: "source_b", ID: "ID_b"}
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))

	reqConditionForAgg := func() bool {
		cacheManager.gvksToSync.IsPresent(configMapGVK)
		gvks := cacheManager.gvksToSync.List(syncSourceOne)
		if len(gvks) != 1 {
			return false
		}
		_, found := gvks[configMapGVK]
		if !found {
			return false
		}

		gvks2 := cacheManager.gvksToSync.List(syncSourceTwo)
		if len(gvks2) != 1 {
			return false
		}
		_, found2 := gvks2[configMapGVK]
		return found2
	}
	require.Eventually(t, reqConditionForAgg, eventuallyTimeout, eventuallyTicker)

	require.NoError(t, cacheManager.AddSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
	expected2 := map[fakes.OpaKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected2), eventuallyTimeout, eventuallyTicker)

	// now go on and unreference sourceTwo's gvks; this should schedule the config maps to be removed
	require.NoError(t, cacheManager.AddSource(ctx, syncSourceTwo, []schema.GroupVersionKind{}))
	expected3 := map[fakes.OpaKey]interface{}{
		// config maps no longer required by any sync source
		// {Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		// {Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}: nil,
	}
	require.Eventually(t, expectedCheck(opaClient, expected3), eventuallyTimeout, eventuallyTicker)

	// now remove all the sources
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))

	// and expect an empty cache and empty aggregator
	require.Eventually(t, expectedCheck(opaClient, map[fakes.OpaKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	require.True(t, len(cacheManager.gvksToSync.GVKs()) == 0)

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
	require.Eventually(t, expectedCheck(opaClient, expected), eventuallyTimeout, eventuallyTicker)

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

	require.Eventually(t, expectedCheck(opaClient, map[fakes.OpaKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	// make sure the gvk is still in gvkAggregator
	require.True(t, len(cacheManager.gvksToSync.GVKs()) == 1)
	require.True(t, cacheManager.gvksToSync.IsPresent(configMapGVK))

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
