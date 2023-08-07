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
)

var cfg *rest.Config

var (
	configMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	podGVK       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	configInstancOne      = "config-test-1"
	configInstancTwo      = "config-test-2"
	nsedConfigInstanceOne = "default/config-test-1"
	nsedConfigInstanceTwo = "default/config-test-2"

	podInstanceOne     = "pod-test-1"
	nsedPodInstanceOne = "default/pod-test-1"
)

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

type failureInjector struct {
	mu       sync.Mutex
	failures map[string]int // registers GVK.Kind and how many times to fail
}

func (f *failureInjector) setFailures(kind string, failures int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failures[kind] = failures
}

// checkFailures looks at the count of failures and returns true
// if there are still failures for the kind to consume, false otherwise.
func (f *failureInjector) checkFailures(kind string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, ok := f.failures[kind]
	if !ok {
		return false
	}

	if v == 0 {
		return false
	}

	f.failures[kind] = v - 1

	return true
}

func newFailureInjector() *failureInjector {
	return &failureInjector{
		failures: make(map[string]int),
	}
}

// TestCacheManager_replay_retries tests that we can retry GVKs that error out in the replay goroutine.
func TestCacheManager_replay_retries(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	fi := newFailureInjector()
	reader := fakes.SpyReader{
		Reader: c,
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			// return as many syntenthic failures as there are registered for this kind
			if fi.checkFailures(list.GetObjectKind().GroupVersionKind().Kind) {
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

	cm := unstructuredFor(configMapGVK, configInstancOne)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", configInstancOne))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", configInstancOne))
	})

	pod := unstructuredFor(podGVK, podInstanceOne)
	require.NoError(t, c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", podInstanceOne))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, pod), fmt.Sprintf("deleting resource %s", podInstanceOne))
	})

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK, podGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
		{Gvk: podGVK, Key: nsedPodInstanceOne}:          nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	fi.setFailures("ConfigMapList", 5)

	// this call should schedule a cache wipe and a replay for the configMapGVK
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected2 := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
	}
	require.Eventually(t, expectedCheck(cfClient, expected2), eventuallyTimeout, eventuallyTicker)
}

// TestCacheManager_concurrent makes sure that we can add and remove multiple sources
// from separate go routines and changes to the underlying cache are reflected.
func TestCacheManager_concurrent(t *testing.T) {
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())
	testResources, ctx := makeTestResources(t, mgr, wm, c)

	cacheManager := testResources.CacheManager
	dataStore := testResources.CFDataClient
	agg := testResources.GVKAgreggator

	// Create configMaps to test for
	cm := unstructuredFor(configMapGVK, configInstancOne)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", configInstancOne))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", configInstancOne))
	})

	cm2 := unstructuredFor(configMapGVK, configInstancTwo)
	require.NoError(t, c.Create(ctx, cm2), fmt.Sprintf("creating ConfigMap %s", configInstancTwo))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm2), fmt.Sprintf("deleting resource %s", configInstancTwo))
	})

	pod := unstructuredFor(podGVK, podInstanceOne)
	require.NoError(t, c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", podInstanceOne))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, pod), fmt.Sprintf("deleting resource %s", podInstanceOne))
	})

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
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
		{Gvk: configMapGVK, Key: nsedConfigInstanceTwo}: nil,
		{Gvk: podGVK, Key: nsedPodInstanceOne}:          nil,
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
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))

	// expect the config map instances to be repopulated eventually
	expected = map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
		{Gvk: configMapGVK, Key: nsedConfigInstanceTwo}: nil,
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
	for i := 1; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()

			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))
		}()
		go func() {
			defer wg.Done()

			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{podGVK}))
			require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{configMapGVK}))
		}()
	}

	wg.Wait()

	// final upsert for determinism
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceTwo, []schema.GroupVersionKind{podGVK}))

	expected = map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
		{Gvk: configMapGVK, Key: nsedConfigInstanceTwo}: nil,
		{Gvk: podGVK, Key: nsedPodInstanceOne}:          nil,
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

	cm := unstructuredFor(configMapGVK, configInstancOne)
	require.NoError(t, c.Create(ctx, cm), fmt.Sprintf("creating ConfigMap %s", configInstancOne))
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), fmt.Sprintf("deleting resource %s", configInstancOne))
	})

	syncSourceOne := aggregator.Key{Source: "source_a", ID: "ID_a"}
	require.NoError(t, cacheManager.UpsertSource(ctx, syncSourceOne, []schema.GroupVersionKind{configMapGVK}))

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: configMapGVK, Key: nsedConfigInstanceOne}: nil,
	}

	require.Eventually(t, expectedCheck(cfClient, expected), eventuallyTimeout, eventuallyTicker)

	cm2 := unstructuredFor(configMapGVK, configInstancOne)
	cm2.SetLabels(map[string]string{"testlabel": "test"}) // trigger an instance update
	require.NoError(t, c.Update(ctx, cm2))

	require.Eventually(t, func() bool {
		instance := cfClient.GetData(cachemanager.CfDataKey{Gvk: configMapGVK, Key: nsedConfigInstanceOne})
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
	})

	return testResources{cacheManager, cfClient, aggregator}, ctx
}
