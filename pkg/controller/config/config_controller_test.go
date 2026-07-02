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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
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
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const timeout = time.Second * 20

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	skipNameValidation := true
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Controller:     config.Controller{SkipNameValidation: &skipNameValidation},
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
	defer cancelFunc()

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
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	dataClient := &fakes.FakeCfClient{}

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)
	events := make(chan event.GenericEvent, 1024)
	syncMetricsCache := syncutil.NewMetricsCache()
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events)
	require.NoError(t, err)
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         dataClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           c,
	})
	require.NoError(t, err)

	// start the cache manager
	go func() {
		assert.NoError(t, cacheManager.Start(ctx))
	}()

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	rec, err := newReconciler(mgr, cacheManager, tracker, func(context.Context) (*v1.Pod, error) { return pod, nil }, nil)
	require.NoError(t, err)

	// Wrap the Controller Reconcile function so it writes each request to a map when it is finished reconciling.
	recFn, requests := testutils.SetupTestReconcile(rec)
	require.NoError(t, add(mgr, recFn))

	testutils.StartManager(ctx, t, mgr)

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
	g.Eventually(func() bool {
		expectedReq := reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      "config",
			Namespace: "gatekeeper-system",
		}}
		_, ok := requests.Load(expectedReq)

		return ok
	}).WithTimeout(timeout).Should(gomega.BeTrue())

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

	// directly call cacheManager to avoid any race condition
	// between adding the pod and the sync_controller calling AddObject
	require.NoError(t, cacheManager.AddObject(ctx, fooPod))

	// fooPod should be namespace excluded, hence not added to the cache
	require.False(t, dataClient.Contains(map[fakes.CfDataKey]interface{}{{Gvk: fooPod.GroupVersionKind(), Key: "default"}: struct{}{}}))
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
			Name:      "config",
			Namespace: "gatekeeper-system",
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
	defer cancelFunc()

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
	pod.Spec = v1.PodSpec{
		Containers: []v1.Container{
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

func setupController(ctx context.Context, mgr manager.Manager, wm *watch.Manager, tracker *readiness.Tracker, events chan event.GenericEvent, reader client.Reader, useFakeClient bool) (cachemanager.CFDataClient, error) {
	// initialize constraint framework data client
	var client cachemanager.CFDataClient
	if useFakeClient {
		client = &fakes.FakeCfClient{}
	} else {
		driver, err := rego.New(rego.Tracing(true))
		if err != nil {
			return nil, fmt.Errorf("unable to set up Driver: %w", err)
		}

		client, err = constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints("audit.gatekeeper.sh"))
		if err != nil {
			return nil, fmt.Errorf("unable to set up constraint framework data client: %w", err)
		}
	}

	processExcluder := process.Get()
	syncMetricsCache := syncutil.NewMetricsCache()
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events)
	if err != nil {
		return nil, fmt.Errorf("cannot create registrar: %w", err)
	}
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         client,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           reader,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating cache manager: %w", err)
	}
	go func() {
		_ = cacheManager.Start(ctx)
	}()

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	rec, err := newReconciler(mgr, cacheManager, tracker, func(context.Context) (*v1.Pod, error) { return pod, nil }, nil)
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
	return client, nil
}

// Verify the constraint framework cache is populated based on the config resource.
func TestConfig_CacheContents(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

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
	cmKey, err := fakes.KeyFor(cm)
	require.NoError(t, err)

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	cm2.SetNamespace("kube-system")
	require.NoError(t, c.Create(ctx, cm2), "creating configMap config-test-2")
	t.Cleanup(func() {
		assert.NoError(t, deleteResource(ctx, c, cm2), "deleting configMap config-test-2")
	})
	cm2Key, err := fakes.KeyFor(cm2)
	require.NoError(t, err)

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	require.NoError(t, err)

	events := make(chan event.GenericEvent, 1024)
	dataClient, err := setupController(ctx, mgr, wm, tracker, events, c, true)
	require.NoError(t, err, "failed to set up controller")

	fakeClient, ok := dataClient.(*fakes.FakeCfClient)
	require.True(t, ok)

	testutils.StartManager(ctx, t, mgr)

	// Create the Config object and expect the Reconcile to be created
	config := configFor([]schema.GroupVersionKind{nsGVK, configMapGVK})
	require.NoError(t, c.Create(ctx, config), "creating Config config")

	expected := map[fakes.CfDataKey]interface{}{
		{Gvk: nsGVK, Key: "default"}: nil,
		cmKey:                        nil,
		// kube-system namespace is being excluded, it should not be in the cache
	}
	g.Eventually(func() bool {
		return fakeClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial cache contents")
	require.True(t, fakeClient.HasGVK(nsGVK), "want fakeClient.HasGVK(nsGVK) to be true but got false")

	// Reconfigure to drop the namespace watches
	config = configFor([]schema.GroupVersionKind{configMapGVK})
	configUpdate := config.DeepCopy()

	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(configUpdate), configUpdate))
	configUpdate.Spec = config.Spec
	require.NoError(t, c.Update(ctx, configUpdate), "updating Config config")

	// Expect namespaces to go away from cache
	g.Eventually(func() bool {
		return fakeClient.HasGVK(nsGVK)
	}, 10*time.Second).Should(gomega.BeFalse())

	// Expect our configMap to return at some point
	// TODO: In the future it will remain instead of having to repopulate.
	expected = map[fakes.CfDataKey]interface{}{
		cmKey: nil,
	}
	g.Eventually(func() bool {
		return fakeClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "waiting for ConfigMap to repopulate in cache")

	expected = map[fakes.CfDataKey]interface{}{
		cm2Key: nil,
	}
	g.Eventually(func() bool {
		return !fakeClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "kube-system namespace is excluded. kube-system/config-test-2 should not be in the cache")

	// Delete the config resource - expect cache to empty out.
	if fakeClient.Len() == 0 {
		t.Fatal("sanity")
	}
	require.NoError(t, c.Delete(ctx, config), "deleting Config resource")

	// The cache will be cleared out.
	g.Eventually(func() int {
		return fakeClient.Len()
	}, 10*time.Second).Should(gomega.BeZero(), "waiting for cache to empty")
}

func TestConfig_Retries(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

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

	dataClient := &fakes.FakeCfClient{}
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)

	events := make(chan event.GenericEvent, 1024)
	syncMetricsCache := syncutil.NewMetricsCache()
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events)
	require.NoError(t, err)
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         dataClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           c,
	})
	require.NoError(t, err)
	go func() {
		assert.NoError(t, cacheManager.Start(ctx))
	}()

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	rec, _ := newReconciler(mgr, cacheManager, tracker, func(context.Context) (*v1.Pod, error) { return pod, nil }, nil)
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
	fi := fakes.NewFailureInjector()
	rec.reader = fakes.SpyReader{
		Reader: mgr.GetCache(),
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			// return as many syntenthic failures as there are registered for this kind
			if fi.CheckFailures(list.GetObjectKind().GroupVersionKind().Kind) {
				return fmt.Errorf("synthetic failure")
			}

			return mgr.GetCache().List(ctx, list, opts...)
		},
	}

	testutils.StartManager(ctx, t, mgr)

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
	cmKey, err := fakes.KeyFor(cm)
	require.NoError(t, err)

	expected := map[fakes.CfDataKey]interface{}{
		cmKey: nil,
	}
	g.Eventually(func() bool {
		return dataClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial cache contents")

	fi.SetFailures("ConfigMapList", 2)

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
		return dataClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking final cache contents")
}

func TestTriggerConstraintTemplateReconciliation(t *testing.T) {
	ctx := context.Background()

	// Helper to create a valid VAP-enabled constraint template
	createVAPTemplate := func(name string) *v1beta1.ConstraintTemplate {
		// Create a proper CEL source that enables VAP generation
		source := &celSchema.Source{
			Validations: []celSchema.Validation{
				{
					Expression: "true",
					Message:    "test validation",
				},
			},
			GenerateVAP: ptr.To(true),
		}

		ct := &v1beta1.ConstraintTemplate{}
		ct.SetName(name)
		ct.Spec.CRD.Spec.Names.Kind = name + "Kind"
		// Add K8sNativeValidation target to make it VAP-eligible
		ct.Spec.Targets = []v1beta1.Target{
			{
				Target: "admission.k8s.gatekeeper.sh",
				Code: []v1beta1.Code{
					{
						Engine: "K8sNativeValidation",
						Source: &templates.Anything{
							Value: source.MustToUnstructured(),
						},
					},
				},
			},
		}
		return ct
	}

	t.Run("successful reconciliation with valid templates", func(t *testing.T) {
		// Setup
		mgr, _ := setupManager(t)
		_, err := readiness.SetupTracker(mgr, false, false, false)
		require.NoError(t, err)

		// Create a mock reader that returns constraint templates
		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if ctList, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					ct1 := createVAPTemplate("test-template-1")
					ct2 := createVAPTemplate("test-template-2")
					ctList.Items = []v1beta1.ConstraintTemplate{*ct1, *ct2}
				}
				return nil
			},
		}

		// Create event channel
		ctEvents := make(chan event.GenericEvent, 10)

		// Create reconciler
		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err = reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.NoError(t, err)

		assert.Equal(t, 2, len(ctEvents))

		// Verify the events contain the correct templates
		event1 := <-ctEvents
		event2 := <-ctEvents

		ct1, ok1 := event1.Object.(*v1beta1.ConstraintTemplate)
		ct2, ok2 := event2.Object.(*v1beta1.ConstraintTemplate)

		assert.True(t, ok1)
		assert.True(t, ok2)
		assert.Contains(t, []string{"test-template-1", "test-template-2"}, ct1.GetName())
		assert.Contains(t, []string{"test-template-1", "test-template-2"}, ct2.GetName())
	})

	t.Run("handles reader error gracefully", func(t *testing.T) {
		// Setup
		mgr, _ := setupManager(t)

		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				return fmt.Errorf("mock list error")
			},
		}

		ctEvents := make(chan event.GenericEvent, 10)

		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.Error(t, err)

		assert.Equal(t, 0, len(ctEvents))
	})

	t.Run("skips templates without VAP support", func(t *testing.T) {
		// Setup
		mgr, _ := setupManager(t)

		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if ctList, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					// Create a constraint template WITHOUT K8sNativeValidation engine
					// This means it won't generate VAP
					ct := &v1beta1.ConstraintTemplate{}
					ct.SetName("skip-template")
					ct.Spec.CRD.Spec.Names.Kind = "SkipConstraint"
					// Only Rego target, no K8sNativeValidation
					ct.Spec.Targets = []v1beta1.Target{
						{
							Target: "admission.k8s.gatekeeper.sh",
							Code: []v1beta1.Code{
								{
									Engine: "Rego",
									Source: &templates.Anything{},
								},
							},
						},
					}
					ctList.Items = []v1beta1.ConstraintTemplate{*ct}
				}
				return nil
			},
		}

		ctEvents := make(chan event.GenericEvent, 10)

		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.NoError(t, err)

		assert.Equal(t, 0, len(ctEvents))
	})

	t.Run("handles full event channel with retry", func(t *testing.T) {
		mgr, _ := setupManager(t)

		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if ctList, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					ct := createVAPTemplate("test-template")
					ctList.Items = []v1beta1.ConstraintTemplate{*ct}
				}
				return nil
			},
		}

		ctEvents := make(chan event.GenericEvent)

		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "channel is full")
	})

	t.Run("handles empty template list", func(t *testing.T) {
		// Setup
		mgr, _ := setupManager(t)

		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				// Return empty list
				return nil
			},
		}

		ctEvents := make(chan event.GenericEvent, 10)

		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.NoError(t, err)

		assert.Equal(t, 0, len(ctEvents))
	})

	t.Run("handles nil ctEvents channel", func(t *testing.T) {
		// Setup
		mgr, _ := setupManager(t)

		mockReader := &fakes.SpyReader{
			ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if ctList, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					ct := createVAPTemplate("test-template")
					ctList.Items = []v1beta1.ConstraintTemplate{*ct}
				}
				return nil
			},
		}

		reconciler := &ReconcileConfig{
			reader:   mockReader,
			scheme:   mgr.GetScheme(),
			ctEvents: nil, // nil channel
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		assert.NoError(t, err)
	})
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

func TestDirtyTemplateManagement(t *testing.T) {
	t.Run("markDirtyTemplate adds template to dirty state", func(t *testing.T) {
		r := &ReconcileConfig{}

		template := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		}

		r.markDirtyTemplate(template)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.NotNil(t, r.dirtyTemplates)
		require.Contains(t, r.dirtyTemplates, "test-template")
		require.Equal(t, template, r.dirtyTemplates["test-template"])
	})

	t.Run("markDirtyTemplate handles multiple templates", func(t *testing.T) {
		r := &ReconcileConfig{}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Len(t, r.dirtyTemplates, 2)
		require.Contains(t, r.dirtyTemplates, "template-1")
		require.Contains(t, r.dirtyTemplates, "template-2")
	})

	t.Run("getDirtyTemplatesAndClear returns all dirty templates and clears state", func(t *testing.T) {
		r := &ReconcileConfig{}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		dirtyTemplates := r.getDirtyTemplatesAndClear()

		require.Len(t, dirtyTemplates, 2)
		templateNames := make([]string, len(dirtyTemplates))
		for i, template := range dirtyTemplates {
			templateNames[i] = template.Name
		}
		require.Contains(t, templateNames, "template-1")
		require.Contains(t, templateNames, "template-2")

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Empty(t, r.dirtyTemplates)
	})

	t.Run("getDirtyTemplatesAndClear returns nil when no dirty templates", func(t *testing.T) {
		r := &ReconcileConfig{}

		dirtyTemplates := r.getDirtyTemplatesAndClear()

		require.Nil(t, dirtyTemplates)
	})
}

func TestTriggerDirtyTemplateReconciliation(t *testing.T) {
	t.Run("processes dirty templates and sends events", func(t *testing.T) {
		ctEvents := make(chan event.GenericEvent, 10)
		r := &ReconcileConfig{ctEvents: ctEvents}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		ctx := context.Background()
		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.NoError(t, err)
		require.Len(t, ctEvents, 2)

		receivedEvents := make([]event.GenericEvent, 2)
		receivedEvents[0] = <-ctEvents
		receivedEvents[1] = <-ctEvents

		receivedNames := make([]string, 2)
		for i, evt := range receivedEvents {
			if template, ok := evt.Object.(*v1beta1.ConstraintTemplate); ok {
				receivedNames[i] = template.Name
			}
		}
		require.Contains(t, receivedNames, "template-1")
		require.Contains(t, receivedNames, "template-2")

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Empty(t, r.dirtyTemplates)
	})

	t.Run("re-marks failed templates as dirty", func(t *testing.T) {
		ctEvents := make(chan event.GenericEvent)
		r := &ReconcileConfig{ctEvents: ctEvents}

		template := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		}

		r.markDirtyTemplate(template)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.Error(t, err)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Contains(t, r.dirtyTemplates, "test-template")
	})

	t.Run("returns nil when no dirty templates", func(t *testing.T) {
		r := &ReconcileConfig{}

		ctx := context.Background()
		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.NoError(t, err)
	})
}
