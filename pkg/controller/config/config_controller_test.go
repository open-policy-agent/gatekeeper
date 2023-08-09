/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{
	Name:      "config",
	Namespace: "gatekeeper-system",
}}

const timeout = time.Second * 20

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		MapperProvider:     apiutil.NewDynamicRESTMapper,
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		t.Fatalf("expected dynamic cache, got: %T", c)
	}
	wm, err := watch.New(dc)
	if err != nil {
		t.Fatalf("could not create watch manager: %s", err)
	}
	if err := mgr.Add(wm); err != nil {
		t.Fatalf("could not add watch manager to manager: %s", err)
	}
	return mgr, wm
}

func TestReconcile(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	g := gomega.NewGomegaWithT(t)

	instance := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Namespace"},
					{Group: "", Version: "v1", Kind: "Pod"},
				},
			},
			Match: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"foo"},
					Processes:          []string{"*"},
				},
				{
					ExcludedNamespaces: []wildcard.Wildcard{"bar"},
					Processes:          []string{"audit", "webhook"},
				},
			},
		},
	}

	// Set up the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	opaClient := &cachemanager.FakeCfClient{}

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)
	events := make(chan event.GenericEvent, 1024)
	watchSet := watch.NewSet()
	syncMetricsCache := syncutil.NewMetricsCache()
	w, err := wm.NewRegistrar(
		CtrlName,
		events)
	require.NoError(t, err)
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         opaClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watchSet,
		Registrar:        w,
		Reader:           c,
	})
	require.NoError(t, err)

	// start the cache manager
	go func() {
		require.NoError(t, cacheManager.Start(ctx))
	}()

	rec, err := newReconciler(mgr, cacheManager, wm, cs, tracker, processExcluder, watchSet)
	require.NoError(t, err)

	recFn, requests := SetupTestReconcile(rec)
	require.NoError(t, add(mgr, recFn))

	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	err = c.Create(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		ctx := context.Background()
		err = c.Delete(ctx, instance)
		if err != nil {
			t.Fatal(err)
		}
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	g.Eventually(func() int {
		return len(wm.GetManagedGVK())
	}).WithTimeout(timeout).ShouldNot(gomega.Equal(0))
	gvks := wm.GetManagedGVK()

	wantGVKs := []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Namespace"},
		{Group: "", Version: "v1", Kind: "Pod"},
	}
	if diff := cmp.Diff(wantGVKs, gvks); diff != "" {
		t.Fatal(diff)
	}

	ns := &unstructured.Unstructured{}
	ns.SetName("testns")
	nsGvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	ns.SetGroupVersionKind(nsGvk)
	err = c.Create(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}

	fooNS := &unstructured.Unstructured{}
	fooNS.SetName("foo")
	fooNS.SetGroupVersionKind(nsGvk)
	auditExcludedNS, _ := processExcluder.IsNamespaceExcluded(process.Audit, fooNS)
	if !auditExcludedNS {
		t.Fatal("got false but want true")
	}
	syncExcludedNS, _ := processExcluder.IsNamespaceExcluded(process.Sync, fooNS)
	if !syncExcludedNS {
		t.Fatal("got false but want true")
	}
	webhookExcludedNS, _ := processExcluder.IsNamespaceExcluded(process.Webhook, fooNS)
	if !webhookExcludedNS {
		t.Fatal("got false but want true")
	}

	barNS := &unstructured.Unstructured{}
	barNS.SetName("bar")
	barNS.SetGroupVersionKind(nsGvk)
	syncNotExcludedNS, err := processExcluder.IsNamespaceExcluded(process.Sync, barNS)
	if syncNotExcludedNS {
		t.Fatal("got true but want false")
	}
	if err != nil {
		t.Fatal(err)
	}

	fooPod := &unstructured.Unstructured{}
	fooPod.SetName("foo")
	fooPod.SetNamespace("foo")
	podGvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	fooPod.SetGroupVersionKind(podGvk)
	auditExcludedPod, _ := processExcluder.IsNamespaceExcluded(process.Audit, fooPod)
	if !auditExcludedPod {
		t.Fatal("got false but want true")
	}
	syncExcludedPod, _ := processExcluder.IsNamespaceExcluded(process.Sync, fooPod)
	if !syncExcludedPod {
		t.Fatal("got false but want true")
	}
	webhookExcludedPod, _ := processExcluder.IsNamespaceExcluded(process.Webhook, fooPod)
	if !webhookExcludedPod {
		t.Fatal("got false but want true")
	}

	barPod := &unstructured.Unstructured{}
	barPod.SetName("bar")
	barPod.SetNamespace("bar")
	barPod.SetGroupVersionKind(podGvk)
	syncNotExcludedPod, err := processExcluder.IsNamespaceExcluded(process.Sync, barPod)
	if syncNotExcludedPod {
		t.Fatal("got true but want false")
	}
	if err != nil {
		t.Fatal(err)
	}

	fooNs := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	require.NoError(t, c.Create(ctx, fooNs))
	fooPod.Object["spec"] = map[string]interface{}{
		"containers": []map[string]interface{}{
			{
				"name":  "foo-container",
				"image": "foo-image",
			},
		},
	}
	require.NoError(t, c.Create(ctx, fooPod))
	// fooPod should be namespace excluded, hence not synced
	g.Eventually(opaClient.Contains(map[cachemanager.CfDataKey]interface{}{{Gvk: fooPod.GroupVersionKind(), Key: "default"}: struct{}{}}), timeout).ShouldNot(gomega.BeTrue())

	require.NoError(t, c.Delete(ctx, fooPod))
	testMgrStopped()
	cs.Stop()
}

// tests that expectations for sync only resource gets canceled when it gets deleted.
func TestConfig_DeleteSyncResources(t *testing.T) {
	log.Info("Running test: Cancel the expectations when sync only resource gets deleted")

	g := gomega.NewGomegaWithT(t)

	// setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// create the Config object and expect the Reconcile to be created when controller starts
	instance := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "config",
			Namespace:  "gatekeeper-system",
			Finalizers: []string{finalizerName},
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Pod"},
				},
			},
		},
	}
	ctx, cancelFunc := context.WithCancel(context.Background())
	err := c.Create(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		ctx := context.Background()
		err = c.Delete(ctx, instance)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// create the pod that is a sync only resource in config obj
	pod := fakes.Pod(
		fakes.WithNamespace("default"),
		fakes.WithName("testpod"),
	)
	pod.Spec = corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		},
	}

	err = c.Create(ctx, pod)
	if err != nil {
		t.Fatal(err)
	}

	// set up tracker
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	// events channel will be used to receive events from dynamic watches
	events := make(chan event.GenericEvent, 1024)

	// set up controller and add it to the manager
	_, err = setupController(ctx, mgr, wm, tracker, events, c, false)
	require.NoError(t, err, "failed to set up controller")

	// start manager that will start tracker and controller
	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	defer func() {
		once.Do(func() {
			cancelFunc()
		})
	}()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	// get the object tracker for the synconly pod resource
	tr, ok := tracker.ForData(gvk).(testExpectations)
	if !ok {
		t.Fatalf("unexpected tracker, got %T", tr)
	}

	// ensure that expectations are set for the constraint gvk
	g.Eventually(func() bool {
		return tr.IsExpecting(gvk, types.NamespacedName{Name: "testpod", Namespace: "default"})
	}, timeout).Should(gomega.BeTrue())

	// delete the pod , the delete event will be reconciled by sync controller
	// to cancel the expectation set for it by tracker
	err = c.Delete(ctx, pod)
	if err != nil {
		t.Fatal(err)
	}

	// register events for the pod to go in the event channel
	podObj := fakes.Pod(
		fakes.WithNamespace("default"),
		fakes.WithName("testpod"),
	)

	events <- event.GenericEvent{
		Object: podObj,
	}

	// check readiness tracker is satisfied post-reconcile
	g.Eventually(func() bool {
		return tracker.ForData(gvk).Satisfied()
	}, timeout).Should(gomega.BeTrue())
}

func setupController(ctx context.Context, mgr manager.Manager, wm *watch.Manager, tracker *readiness.Tracker, events chan event.GenericEvent, reader client.Reader, useFakeOpa bool) (cachemanager.CFDataClient, error) {
	// initialize OPA
	var opaClient cachemanager.CFDataClient
	if useFakeOpa {
		opaClient = &cachemanager.FakeCfClient{}
	} else {
		driver, err := rego.New(rego.Tracing(true))
		if err != nil {
			return nil, fmt.Errorf("unable to set up Driver: %w", err)
		}

		opaClient, err = constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
		if err != nil {
			return nil, fmt.Errorf("unable to set up OPA backend client: %w", err)
		}
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	cs := watch.NewSwitch()
	processExcluder := process.Get()
	watchSet := watch.NewSet()
	syncMetricsCache := syncutil.NewMetricsCache()
	w, err := wm.NewRegistrar(
		CtrlName,
		events)
	if err != nil {
		return nil, fmt.Errorf("cannot create registrar: %w", err)
	}
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         opaClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watchSet,
		Registrar:        w,
		Reader:           reader,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating cache manager: %w", err)
	}
	go func() {
		_ = cacheManager.Start(ctx)
	}()

	rec, err := newReconciler(mgr, cacheManager, wm, cs, tracker, processExcluder, watchSet)
	if err != nil {
		return nil, fmt.Errorf("creating reconciler: %w", err)
	}
	err = add(mgr, rec)
	if err != nil {
		return nil, fmt.Errorf("adding reconciler to manager: %w", err)
	}

	syncAdder := syncc.Adder{
		Events:       events,
		CacheManager: cacheManager,
	}
	err = syncAdder.Add(mgr)
	if err != nil {
		return nil, fmt.Errorf("registering sync controller: %w", err)
	}
	return opaClient, nil
}

// Verify the Opa cache is populated based on the config resource.
func TestConfig_CacheContents(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	// Setup the Manager and Controller.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())
	g := gomega.NewGomegaWithT(t)
	nsGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	}
	configMapGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
	// Create a configMap to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("default")
	require.NoError(t, c.Create(ctx, cm), "creating configMap config-test-1")
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm), "deleting configMap config-test-1")
	})
	cmKey, err := cachemanager.KeyFor(cm)
	require.NoError(t, err)

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	cm2.SetNamespace("kube-system")
	require.NoError(t, c.Create(ctx, cm2), "creating configMap config-test-2")
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm2), "deleting configMap config-test-2")
	})
	cm2Key, err := cachemanager.KeyFor(cm2)
	require.NoError(t, err)

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	require.NoError(t, err)

	events := make(chan event.GenericEvent, 1024)
	opa, err := setupController(ctx, mgr, wm, tracker, events, c, true)
	require.NoError(t, err, "failed to set up controller")

	opaClient, ok := opa.(*cachemanager.FakeCfClient)
	require.True(t, ok)

	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}
	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	config := configFor([]schema.GroupVersionKind{nsGVK, configMapGVK})
	require.NoError(t, c.Create(ctx, config), "creating Config config")

	expected := map[cachemanager.CfDataKey]interface{}{
		{Gvk: nsGVK, Key: "default"}: nil,
		cmKey:                        nil,
		// kube-system namespace is being excluded, it should not be in opa cache
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial opa cache contents")
	require.True(t, opaClient.HasGVK(nsGVK), "want opaClient.HasGVK(nsGVK) to be true but got false")

	// Reconfigure to drop the namespace watches
	config = configFor([]schema.GroupVersionKind{configMapGVK})
	configUpdate := config.DeepCopy()

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(configUpdate), configUpdate))
	configUpdate.Spec = config.Spec
	require.NoError(t, c.Update(ctx, configUpdate), "updating Config config")

	// Expect namespaces to go away from cache
	g.Eventually(func() bool {
		return opaClient.HasGVK(nsGVK)
	}, 10*time.Second).Should(gomega.BeFalse())

	// Expect our configMap to return at some point
	// TODO: In the future it will remain instead of having to repopulate.
	expected = map[cachemanager.CfDataKey]interface{}{
		cmKey: nil,
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "waiting for ConfigMap to repopulate in cache")

	expected = map[cachemanager.CfDataKey]interface{}{
		cm2Key: nil,
	}
	g.Eventually(func() bool {
		return !opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "kube-system namespace is excluded. kube-system/config-test-2 should not be in opa cache")

	// Delete the config resource - expect opa to empty out.
	if opaClient.Len() == 0 {
		t.Fatal("sanity")
	}
	require.NoError(t, c.Delete(ctx, config), "deleting Config resource")

	// The cache will be cleared out.
	g.Eventually(func() int {
		return opaClient.Len()
	}, 10*time.Second).Should(gomega.BeZero(), "waiting for cache to empty")
}

func TestConfig_Retries(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	g := gomega.NewGomegaWithT(t)
	nsGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	}
	configMapGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
	instance := configFor([]schema.GroupVersionKind{nsGVK, configMapGVK})

	// Setup the Manager and Controller.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	opaClient := &cachemanager.FakeCfClient{}
	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)

	events := make(chan event.GenericEvent, 1024)
	watchSet := watch.NewSet()
	syncMetricsCache := syncutil.NewMetricsCache()
	w, err := wm.NewRegistrar(
		CtrlName,
		events)
	require.NoError(t, err)
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         opaClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watchSet,
		Registrar:        w,
		Reader:           c,
	})
	require.NoError(t, err)
	go func() {
		require.NoError(t, cacheManager.Start(ctx))
	}()

	rec, _ := newReconciler(mgr, cacheManager, wm, cs, tracker, processExcluder, watchSet)
	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}
	syncAdder := syncc.Adder{
		Events:       events,
		CacheManager: cacheManager,
	}
	require.NoError(t, syncAdder.Add(mgr), "registering sync controller")

	// Use our special reader interceptor to inject controlled failures
	failPlease := make(chan string, 1)
	rec.reader = fakes.SpyReader{
		Reader: mgr.GetCache(),
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
			return mgr.GetCache().List(ctx, list, opts...)
		},
	}

	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	g.Eventually(func() error {
		return c.Create(ctx, instance.DeepCopy())
	}, timeout).Should(gomega.BeNil())

	defer func() {
		ctx := context.Background()
		err = c.Delete(ctx, instance)
		if err != nil {
			t.Error(err)
		}
	}()

	// Create a configMap to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("default")
	err = c.Create(ctx, cm)
	if err != nil {
		t.Fatal("creating configMap config-test-1:", err)
	}

	defer func() {
		err = c.Delete(ctx, cm)
		if err != nil {
			t.Error(err)
		}
	}()
	cmKey, err := cachemanager.KeyFor(cm)
	require.NoError(t, err)

	expected := map[cachemanager.CfDataKey]interface{}{
		cmKey: nil,
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial opa cache contents")

	// Make List fail once for ConfigMaps as the replay occurs following the reconfig below.
	failPlease <- "ConfigMapList"

	// Reconfigure to force an internal replay.
	instance = configFor([]schema.GroupVersionKind{configMapGVK})
	forUpdate := instance.DeepCopy()
	_, err = controllerutil.CreateOrUpdate(ctx, c, forUpdate, func() error {
		forUpdate.Spec = instance.Spec
		return nil
	})
	if err != nil {
		t.Fatalf("updating Config resource: %v", err)
	}

	// Despite the transient error, we expect the cache to eventually be repopulated.
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking final opa cache contents")
}

// configFor returns a config resource that watches the requested set of resources.
func configFor(kinds []schema.GroupVersionKind) *configv1alpha1.Config {
	entries := make([]configv1alpha1.SyncOnlyEntry, len(kinds))
	for i := range kinds {
		entries[i].Group = kinds[i].Group
		entries[i].Version = kinds[i].Version
		entries[i].Kind = kinds[i].Kind
	}

	return &configv1alpha1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configv1alpha1.GroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: entries,
			},
			Match: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"sync"},
				},
			},
		},
	}
}

// unstructuredFor returns an Unstructured resource of the requested kind and name.
func unstructuredFor(gvk schema.GroupVersionKind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	return u
}

// This interface is getting used by tests to check the private objects of objectTracker.
type testExpectations interface {
	IsExpecting(gvk schema.GroupVersionKind, nsName types.NamespacedName) bool
}

func deleteResource(ctx context.Context, c client.Client, resounce *unstructured.Unstructured) error {
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	err := c.Delete(ctx, resounce)
	if apierrors.IsNotFound(err) {
		// resource does not exist, this is good
		return nil
	}

	return err
}
