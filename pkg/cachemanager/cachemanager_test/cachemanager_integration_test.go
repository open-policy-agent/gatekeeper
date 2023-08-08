package cachemanager_test

import (
	"context"
	"fmt"
	"math/rand"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	jitterUpperBound = 100
)

var cfg *rest.Config

var (
	configMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	cm1Name           = "config-test-1"
	cm2Name           = "config-test-2"
	namespacedCm1Name = "default/config-test-1"
	namespacedCm2Name = "default/config-test-2"

	pod1Name           = "pod-test-1"
	namespacedPod1Name = "default/pod-test-1"
)

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

// TestCacheManager_replay_retries tests that we can retry GVKs that error out in the replay goroutine.
func TestCacheManager_replay_retries(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	fi := fakes.NewFailureInjector()
	reader := fakes.SpyReader{
		Reader: c,
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			// return as many syntenthic failures as there are registered for this kind
			if fi.CheckFailures(list.GetObjectKind().GroupVersionKind().Kind) {
				return fmt.Errorf("synthetic failure")
			}

			return c.List(ctx, list, opts...)
		},
	}

	testResources, ctx := makeTestResources(t, mgr, wm, reader)
	cacheManager := testResources.CacheManager
	dataStore := testResources.CFDataClient

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	cm := unstructuredFor(configMapGVK, cm1Name)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", cm1Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", cm1Name))
	})

	pod := unstructuredFor(podGVK, pod1Name)
	require.NoError(t, c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", pod1Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, pod), fmt.Sprintf("deleting resource %s", pod1Name))
	})

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: namespacedCm1Name}: nil,
		{Gvk: podGVK, Key: namespacedPod1Name}:      nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	fi.SetFailures("ConfigMapList", 5)

	// this call should schedule a cache wipe and a replay for the configMapGVK
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected2 := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: namespacedCm1Name}: nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected2), eventuallyTimeout, eventuallyTicker)
}

// TestCacheManager_concurrent makes sure that we can add and remove multiple sources
// from separate go routines and changes to the underlying cache are reflected.
func TestCacheManager_concurrent(t *testing.T) {
	r := rand.New(rand.NewSource(12345)) // #nosec G404: Using weak random number generator for determinism between calls

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())
	testResources, ctx := makeTestResources(t, mgr, wm, c)

	cacheManager := testResources.CacheManager
	dataStore := testResources.CFDataClient
	agg := testResources.GVKAgreggator

	// Create configMaps to test for
	cm := unstructuredFor(configMapGVK, cm1Name)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", cm1Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", cm1Name))
	})

	cm2 := unstructuredFor(configMapGVK, cm2Name)
	require.NoError(t, c.Create(ctx, cm2), fmt.Sprintf("creating ConfigMap %s", cm2Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm2), fmt.Sprintf("deleting resource %s", cm2Name))
	})

	pod := unstructuredFor(podGVK, pod1Name)
	require.NoError(t, c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", pod1Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, pod), fmt.Sprintf("deleting resource %s", pod1Name))
	})

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	syncSourceTwo := aggregator.Key{Source: "source_b", ID: "ID_b"}

	wg := &sync.WaitGroup{}

	// simulate a churn-y concurrent access by swapping the gvks for the sync sources repeatedly
	// and removing sync sources, all from different go routines.
	for i := 1; i < 100; i++ {
		wg.Add(3)

		// add some jitter between go func calls
		time.Sleep(time.Duration(r.Intn(jitterUpperBound)) * time.Millisecond)
		go func() {
			defer wg.Done()

			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))
		}()

		time.Sleep(time.Duration(r.Intn(jitterUpperBound)) * time.Millisecond)
		go func() {
			defer wg.Done()

			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))
		}()

		time.Sleep(time.Duration(r.Intn(jitterUpperBound)) * time.Millisecond)
		go func() {
			defer wg.Done()

			require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))
			require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))
		}()
	}

	wg.Wait()

	// final upsert for determinism
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: namespacedCm1Name}: nil,
		{Gvk: configMapGVK, Key: namespacedCm2Name}: nil,
		{Gvk: podGVK, Key: namespacedPod1Name}:      nil,
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

	// do a final remove and expect the cache to clear
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceOne))
	require.NoError(t, cacheManager.RemoveSource(ctx, syncSourceTwo))

	require.Eventually(t, expectedCheck(cfClient, map[cachemanager.CfDataKey]interface{}{}), eventuallyTimeout, eventuallyTicker)
	require.True(t, len(agg.GVKs()) == 0)
}

// TestCacheManager_instance_updates tests that cache manager wires up dependencies correctly
// such that updates to an instance of a watched gvks are reconciled in the sync_controller.
func TestCacheManager_instance_updates(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	testResources, ctx := makeTestResources(t, mgr, wm, c)

	cacheManager := testResources.CacheManager
	dataStore := testResources.CFDataClient

	cfClient, ok := dataStore.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	cm := unstructuredFor(configMapGVK, cm1Name)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", cm1Name))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", cm1Name))
	})

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: namespacedCm1Name}: nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	cmUpdate := unstructuredFor(configMapGVK, cm1Name)
	cmUpdate.SetLabels(map[string]string{"testlabel": "test"}) // trigger an instance update
	require.NoError(t, c.Update(ctx, cmUpdate))

	require.Eventually(t, func() bool {
		instance := cfClient.GetData(cachemanager.CfDataKey{Gvk: configMapGVK, Key: namespacedCm1Name})
		unInstance, ok := instance.(*unstructured.Unstructured)
		require.True(t, ok)

		value, found, err := unstructured.NestedString(unInstance.Object, "metadata", "labels", "testlabel")
		require.NoError(t, err)

		return found && "test" == value
	}, eventuallyTimeout, eventuallyTicker)
}

func deleteResource(ctx context.Context, c client.Client, resounce *unstructured.Unstructured) error {
	err := c.Delete(ctx, resounce)
	if apierrors.IsNotFound(err) {
		// resource does not exist, this is good
		return nil
	}

	return err
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

type testResources struct {
	*cachemanager.CacheManager
	cachemanager.CFDataClient
	*aggregator.GVKAgreggator
}

func makeTestResources(t *testing.T, mgr manager.Manager, wm *watch.Manager, reader client.Reader) (testResources, context.Context) {
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
	config := &cachemanager.Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watch.NewSet(),
		Registrar:        w,
		Reader:           reader,
		GVKAggregator:    aggregator,
	}
	cacheManager, err := cachemanager.NewCacheManager(config)
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
	})

	return testResources{cacheManager, cfClient, aggregator}, ctx
}
