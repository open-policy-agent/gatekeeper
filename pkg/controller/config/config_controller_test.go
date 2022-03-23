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
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/test/clients"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
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
		NewCache:           dynamiccache.New,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
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
					ExcludedNamespaces: []util.Wildcard{"foo"},
					Processes:          []string{"*"},
				},
				{
					ExcludedNamespaces: []util.Wildcard{"bar"},
					Processes:          []string{"audit", "webhook"},
				},
			},
		},
	}

	// Set up the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// initialize OPA
	driver, err := local.New(local.Tracing(true))
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	opaClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)
	events := make(chan event.GenericEvent, 1024)
	watchSet := watch.NewSet()
	rec, _ := newReconciler(mgr, opaClient, wm, cs, tracker, processExcluder, events, watchSet, events)

	recFn, requests := SetupTestReconcile(rec)
	err = add(mgr, recFn)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
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

	gvks := wm.GetManagedGVK()
	g.Eventually(len(gvks), timeout).ShouldNot(gomega.Equal(0))

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
	ctx := context.Background()
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
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}

	// events channel will be used to receive events from dynamic watches
	events := make(chan event.GenericEvent, 1024)

	// set up controller and add it to the manager
	err = setupController(mgr, wm, tracker, events)
	if err != nil {
		t.Fatal(err)
	}

	// start manager that will start tracker and controller
	ctx, cancelFunc := context.WithCancel(context.Background())
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

func setupController(mgr manager.Manager, wm *watch.Manager, tracker *readiness.Tracker, events <-chan event.GenericEvent) error {
	// initialize OPA
	driver, err := local.New(local.Tracing(true))
	if err != nil {
		return fmt.Errorf("unable to set up Driver: %v", err)
	}

	opaClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		return fmt.Errorf("unable to set up OPA backend client: %w", err)
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	cs := watch.NewSwitch()

	processExcluder := process.Get()

	watchSet := watch.NewSet()
	rec, _ := newReconciler(mgr, opaClient, wm, cs, tracker, processExcluder, events, watchSet, nil)
	err = add(mgr, rec)
	if err != nil {
		return fmt.Errorf("adding reconciler to manager: %w", err)
	}
	return nil
}

// Verify the Opa cache is populated based on the config resource.
func TestConfig_CacheContents(t *testing.T) {
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
	instance := configFor([]schema.GroupVersionKind{
		nsGVK,
		configMapGVK,
	})

	// Setup the Manager and Controller.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	opaClient := &fakeOpa{}
	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)

	events := make(chan event.GenericEvent, 1024)
	watchSet := watch.NewSet()
	rec, _ := newReconciler(mgr, opaClient, wm, cs, tracker, processExcluder, events, watchSet, events)
	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	ctx = context.Background()

	instance = configFor([]schema.GroupVersionKind{nsGVK, configMapGVK})

	// Since we're reusing instance between tests, we must wait for it to be fully
	// deleted. We also can't reuse the same instance without introducing
	// flakiness as client.Client methods modify their input.
	g.Eventually(ensureDeleted(ctx, c, instance), timeout).
		ShouldNot(gomega.HaveOccurred())
	g.Eventually(ensureCreated(ctx, c, instance), timeout).
		ShouldNot(gomega.HaveOccurred())

	t.Cleanup(func() {
		err = c.Delete(ctx, instance)
		if !apierrors.IsNotFound(err) {
			t.Errorf("got Delete(instance) error %v, want IsNotFound", err)
		}
	})

	// Create a configMap to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("default")
	err = c.Create(ctx, cm)
	if err != nil {
		t.Fatalf("creating configMap config-test-1: %v", err)
	}

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	cm2.SetNamespace("kube-system")
	err = c.Create(ctx, cm2)
	if err != nil {
		t.Fatalf("creating configMap config-test-2: %v", err)
	}

	defer func() {
		err = c.Delete(ctx, cm)
		if err != nil {
			t.Fatal(err)
		}
		err = c.Delete(ctx, cm2)
		if err != nil {
			t.Fatal(err)
		}
	}()

	expected := map[opaKey]interface{}{
		{gvk: nsGVK, key: "default"}:                      nil,
		{gvk: configMapGVK, key: "default/config-test-1"}: nil,
		// kube-system namespace is being excluded, it should not be in opa cache
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial opa cache contents")

	// Sanity
	if !opaClient.HasGVK(nsGVK) {
		t.Fatal("want opaClient.HasGVK(nsGVK) to be true but got false")
	}

	// Reconfigure to drop the namespace watches
	instance = configFor([]schema.GroupVersionKind{configMapGVK})
	forUpdate := instance.DeepCopy()
	_, err = controllerutil.CreateOrUpdate(ctx, c, forUpdate, func() error {
		forUpdate.Spec = instance.Spec
		return nil
	})
	if err != nil {
		t.Fatalf("updating Config resource: %v", err)
	}

	// Expect namespaces to go away from cache
	g.Eventually(func() bool {
		return opaClient.HasGVK(nsGVK)
	}, 10*time.Second).Should(gomega.BeFalse())

	// Expect our configMap to return at some point
	// TODO: In the future it will remain instead of having to repopulate.
	expected = map[opaKey]interface{}{
		{
			gvk: configMapGVK,
			key: "default/config-test-1",
		}: nil,
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "waiting for ConfigMap to repopulate in cache")

	expected = map[opaKey]interface{}{
		{
			gvk: configMapGVK,
			key: "kube-system/config-test-2",
		}: nil,
	}
	g.Eventually(func() bool {
		return !opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "kube-system namespace is excluded. kube-system/config-test-2 should not be in opa cache")

	// Delete the config resource - expect opa to empty out.
	if opaClient.Len() == 0 {
		t.Fatal("sanity")
	}
	err = c.Delete(ctx, instance)
	if err != nil {
		t.Fatalf("deleting Config resource: %v", err)
	}

	// The cache will be cleared out.
	g.Eventually(func() int {
		return opaClient.Len()
	}, 10*time.Second).Should(gomega.BeZero(), "waiting for cache to empty")
}

func TestConfig_Retries(t *testing.T) {
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
	instance := configFor([]schema.GroupVersionKind{
		configMapGVK,
	})

	// Setup the Manager and Controller.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	opaClient := &fakeOpa{}
	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)

	events := make(chan event.GenericEvent, 1024)
	watchSet := watch.NewSet()
	rec, _ := newReconciler(mgr, opaClient, wm, cs, tracker, processExcluder, events, watchSet, events)
	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	// Use our special hookReader to inject controlled failures
	failPlease := make(chan string, 1)
	rec.reader = hookReader{
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

	ctx, cancelFunc := context.WithCancel(context.Background())
	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	ctx = context.Background()
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

	expected := map[opaKey]interface{}{
		{gvk: configMapGVK, key: "default/config-test-1"}: nil,
	}
	g.Eventually(func() bool {
		return opaClient.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial opa cache contents")

	// Wipe the opa cache, we want to see it repopulate despite transient replay errors below.
	_, err = opaClient.RemoveData(ctx, target.WipeData())
	if err != nil {
		t.Fatalf("wiping opa cache: %v", err)
	}
	if opaClient.Contains(expected) {
		t.Fatal("wipe failed")
	}

	// Make List fail once for ConfigMaps as the replay occurs following the reconfig below.
	failPlease <- "ConfigMapList"

	// Reconfigure to add a namespace watch.
	instance = configFor([]schema.GroupVersionKind{nsGVK, configMapGVK})
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
					ExcludedNamespaces: []util.Wildcard{"kube-system"},
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

// ensureDeleted
//
// This package uses the same API server process across multiple test functions.
// The residual state from a previous test function can cause flakes.
//
// To ensure a clean slate, we must verify that any previously applied Config object
// has been fully removed before applying our new object.
func ensureDeleted(ctx context.Context, c client.Client, toDelete client.Object) func() error {
	gvk := toDelete.GetObjectKind().GroupVersionKind()
	key := client.ObjectKeyFromObject(toDelete)

	return func() error {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)

		err := c.Get(ctx, key, u)
		if apierrors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}

		if !u.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("waiting for deletion: %v %v", gvk, key)
		}

		err = c.Delete(ctx, u)
		if err != nil {
			return fmt.Errorf("deleting %v %v: %w", gvk, key, err)
		}

		return fmt.Errorf("queued %v %v for deletion", gvk, key)
	}
}

// ensureCreated attempts to create toCreate in Client c as toCreate existed when ensureCreated was called.
func ensureCreated(ctx context.Context, c client.Client, toCreate client.Object) func() error {
	gvk := toCreate.GetObjectKind().GroupVersionKind()
	key := client.ObjectKeyFromObject(toCreate)

	// As ensureCreated returns a closure, it is possible that the value toCreate will be modified after ensureCreated
	// is called but before the closure is called. Creating a copy here ensures the object to be created is consistent
	// with the way it existed when ensureCreated was called.
	toCreateCopy := toCreate.DeepCopyObject()

	return func() error {
		instance, ok := toCreateCopy.(client.Object)
		if !ok {
			return fmt.Errorf("instance was %T which is not a client.Object", instance)
		}

		err := c.Create(ctx, instance)
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("a copy of %v %v already exists - run ensureDeleted to ensure a fresh copy exists for testing",
				gvk, key)
		} else if err != nil {
			return fmt.Errorf("creating %v %v: %v", gvk, key, err)
		}

		return nil
	}
}
