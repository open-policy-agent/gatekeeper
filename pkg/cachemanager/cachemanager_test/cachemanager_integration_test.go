package cachemanager_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
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

// TestCacheManager_concurrent makes sure that we can add and remove multiple sources
// from separate go routines and changes to the underlying cache are reflected.
func TestCacheManager_concurrent(t *testing.T) {
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
	syncSourceTwo := aggregator.Key{Source: "source_b", ID: "ID_b"}

	wg := &sync.WaitGroup{}

	wg.Add(2)
	go func() {
		defer wg.Done()
		require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
	}()
	go func() {
		defer wg.Done()
		require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))
	}()

	wg.Wait()

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)
	// now assert that the gvkAggregator looks as expected
	agg.IsPresent(configMapGVK)
	gvks := agg.List(syncSourceOne)
	require.Len(t, gvks, 1)
	_, foundConfigMap := gvks[configMapGVK]
	require.True(t, foundConfigMap)
	gvks = agg.List(syncSourceTwo)
	require.Len(t, gvks, 1)
	_, foundPod := gvks[podGVK]
	require.True(t, foundPod)

	// now remove the podgvk for sync source two and make sure we don't have pods in the cache anymore
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))
	}()

	wg.Wait()

	// expecte the config map instances to be repopulated eventually
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

	// now swap the gvks for each source and do so repeatedly to generate some churn
	wg.Add(1)
	go func() {
		defer wg.Done()

		order := true
		for i := 1; i <= 10; i++ {
			if order {
				require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
				require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))
			} else {
				require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
				require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))
			}

			order = !order
		}

		// final upsert for determinism
		require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
		require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))
	}()

	wg.Wait()

	expected = map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: "default/config-test-1"}: nil,
		{Gvk: configMapGVK, Key: "default/config-test-2"}: nil,
		{Gvk: podGVK, Key: "default/pod-1"}:               nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)
	// now assert that the gvkAggregator looks as expected
	agg.IsPresent(configMapGVK)
	gvks = agg.List(syncSourceOne)
	require.Len(t, gvks, 1)
	_, foundConfigMap = gvks[configMapGVK]
	require.True(t, foundConfigMap)
	gvks = agg.List(syncSourceTwo)
	require.Len(t, gvks, 1)
	_, foundPod = gvks[podGVK]
	require.True(t, foundPod)

	// now remove the sources
	wg.Add(2)
	go func() {
		defer wg.Done()
		require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))
	}()
	go func() {
		defer wg.Done()
		require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))
	}()

	wg.Wait()

	// and expect an empty cache and empty aggregator
	require.Eventually(t, expectedCheck(cfClient, map[cachemanager.CfDataKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	require.True(t, len(agg.GVKs()) == 0)

	// cleanup
	require.NoError(t, c.Delete(ctx, cm), "deleting ConfigMap config-test-1")
	require.NoError(t, c.Delete(ctx, cm2), "deleting ConfigMap config-test-2")
	require.NoError(t, c.Delete(ctx, pod), "deleting Pod pod-1")
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

	syncAdder := syncc.Adder{
		Events:       events,
		CacheManager: cacheManager,
	}
	require.NoError(t, syncAdder.Add(mgr), "registering sync controller")
	go func() {
		require.NoError(t, cacheManager.Start(ctx))
	}()

	testutils.StartManager(ctx, t, mgr)

	t.Cleanup(func() {
		cancelFunc()
		ctx.Done()
	})
	return cacheManager, cfClient, aggregator, ctx
}
