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
	"sort"
	gosync "sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
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
			Name:       "config",
			Namespace:  "gatekeeper-system",
			Finalizers: []string{finalizerName},
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
					ExcludedNamespaces: []string{"foo"},
					Processes:          []string{"*"},
				},
				{
					ExcludedNamespaces: []string{"bar"},
					Processes:          []string{"audit", "webhook"},
				},
			},
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := mgr.GetClient()

	// initialize OPA
	driver := local.New(local.Tracing(true))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA backend: %s", err)

	}
	opa, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)
	events := make(chan event.GenericEvent, 1024)
	rec, _ := newReconciler(mgr, opa, wm, cs, tracker, processExcluder, events, events)

	recFn, requests := SetupTestReconcile(rec)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			close(stopMgr)
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		err = c.Delete(context.TODO(), instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	gvks := wm.GetManagedGVK()
	g.Eventually(len(gvks), timeout).ShouldNot(gomega.Equal(0))

	sort.Slice(gvks, func(i, j int) bool { return gvks[i].Kind < gvks[j].Kind })

	g.Expect(gvks).Should(gomega.Equal([]schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Namespace"},
		{Group: "", Version: "v1", Kind: "Pod"},
	}))

	ns := &unstructured.Unstructured{}
	ns.SetName("testns")
	nsGvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	ns.SetGroupVersionKind(nsGvk)
	g.Expect(c.Create(context.TODO(), ns)).NotTo(gomega.HaveOccurred())

	auditExcludedNS := processExcluder.IsNamespaceExcluded(process.Audit, "foo")
	g.Expect(auditExcludedNS).Should(gomega.BeTrue())
	syncExcludedNS := processExcluder.IsNamespaceExcluded(process.Sync, "foo")
	g.Expect(syncExcludedNS).Should(gomega.BeTrue())
	syncNotExcludedNS := processExcluder.IsNamespaceExcluded(process.Sync, "bar")
	g.Expect(syncNotExcludedNS).Should(gomega.BeFalse())
	webhookExcludedNS := processExcluder.IsNamespaceExcluded(process.Webhook, "foo")
	g.Expect(webhookExcludedNS).Should(gomega.BeTrue())

	// Test finalizer removal

	testMgrStopped()
	cs.Stop()
}

// tests that expectations for sync only resource gets cancelled when it gets deleted
func TestConfig_DeleteSyncResources(t *testing.T) {
	log.Info("Running test: Cancel the expectations when sync only resource gets deleted")

	g := gomega.NewGomegaWithT(t)

	// setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := mgr.GetClient()

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
	err := c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		err = c.Delete(context.TODO(), instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	// create the pod that is a sync only resource in config obj
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginx",
				},
			},
		},
	}
	g.Expect(c.Create(context.TODO(), pod)).NotTo(gomega.HaveOccurred())

	// set up tracker
	tracker, err := readiness.SetupTracker(mgr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// events channel will be used to receive events from dynamic watches
	events := make(chan event.GenericEvent, 1024)

	// set up controller and add it to the manager
	err = setupController(mgr, wm, tracker, events)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// start manager that will start tracker and controller
	stopMgr, mgrStopped := StartTestManager(mgr, g)
	once := gosync.Once{}
	defer func() {
		once.Do(func() {
			close(stopMgr)
			mgrStopped.Wait()
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
		return tr.ExpectedContains(gvk, types.NamespacedName{Name: "testpod", Namespace: "default"})
	}, timeout).Should(gomega.BeTrue())

	// delete the pod , the delete event will be reconciled by sync controller
	// to cancel the expectation set for it by tracker
	g.Expect(c.Delete(context.TODO(), pod)).NotTo(gomega.HaveOccurred())

	// register events for the pod to go in the event channel
	podObj := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "default",
		},
	}

	events <- event.GenericEvent{
		Meta:   podObj,
		Object: podObj,
	}

	// check readiness tracker is satisfied post-reconcile
	g.Eventually(func() bool {
		return tracker.ForData(gvk).Satisfied()
	}, timeout).Should(gomega.BeTrue())
}

func setupController(mgr manager.Manager, wm *watch.Manager, tracker *readiness.Tracker, events <-chan event.GenericEvent) error {
	// initialize OPA
	driver := local.New(local.Tracing(true))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		return fmt.Errorf("unable to set up OPA backend: %w", err)
	}

	opa, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		return fmt.Errorf("unable to set up OPA backend client: %w", err)
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	cs := watch.NewSwitch()

	processExcluder := process.Get()

	rec, _ := newReconciler(mgr, opa, wm, cs, tracker, processExcluder, events, nil)
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
	c := mgr.GetClient()

	opa := &fakeOpa{}
	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	processExcluder := process.Get()
	processExcluder.Add(instance.Spec.Match)

	events := make(chan event.GenericEvent, 1024)
	rec, _ := newReconciler(mgr, opa, wm, cs, tracker, processExcluder, events, events)
	g.Expect(add(mgr, rec)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			close(stopMgr)
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	// Create the Config object and expect the Reconcile to be created
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		_ = c.Delete(context.TODO(), instance)
	}()

	// Create a configMap to test for
	cm := unstructuredFor(configMapGVK, "config-test-1")
	cm.SetNamespace("default")
	err = c.Create(context.TODO(), cm)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "creating configMap config-test-1")

	cm2 := unstructuredFor(configMapGVK, "config-test-2")
	cm2.SetNamespace("kube-system")
	err = c.Create(context.TODO(), cm2)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "creating configMap config-test-2")

	defer func() {
		err = c.Delete(context.TODO(), cm)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	expected := map[opaKey]interface{}{
		{gvk: nsGVK, key: "default"}:                      nil,
		{gvk: nsGVK, key: "kube-system"}:                  nil,
		{gvk: configMapGVK, key: "default/config-test-1"}: nil,
	}
	g.Eventually(func() bool {
		return opa.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "checking initial opa cache contents")

	// Sanity
	g.Expect(opa.HasGVK(nsGVK)).To(gomega.BeTrue())

	// Reconfigure to drop the namespace watches
	instance = configFor([]schema.GroupVersionKind{configMapGVK})
	forUpdate := instance.DeepCopy()
	_, err = controllerutil.CreateOrUpdate(context.TODO(), c, forUpdate, func() error {
		forUpdate.Spec = instance.Spec
		return nil
	})
	g.Expect(err).ToNot(gomega.HaveOccurred(), "updating Config resource")

	// Expect namespaces to go away from cache
	g.Eventually(func() bool {
		return opa.HasGVK(nsGVK)
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
		return opa.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "waiting for ConfigMap to repopulate in cache")

	expected = map[opaKey]interface{}{
		{
			gvk: configMapGVK,
			key: "kube-system/config-test-2",
		}: nil,
	}
	g.Eventually(func() bool {
		return !opa.Contains(expected)
	}, 10*time.Second).Should(gomega.BeTrue(), "kube-system namespace is excluded. kube-system/config-test-2 should not be in opa cache")

	// Delete the config resource - expect opa to empty out.
	g.Expect(opa.Len()).ToNot(gomega.BeZero(), "sanity")
	err = c.Delete(context.TODO(), instance)
	g.Expect(err).ToNot(gomega.HaveOccurred(), "deleting Config resource")

	// The cache will be cleared out.
	g.Eventually(func() int {
		return opa.Len()
	}, 10*time.Second).Should(gomega.BeZero(), "waiting for cache to empty")
}

type opaKey struct {
	gvk schema.GroupVersionKind
	key string
}

// fakeOpa is an OpaDataClient for testing.
type fakeOpa struct {
	mu   gosync.Mutex
	data map[opaKey]interface{}
}

// keyFor returns an opaKey for the provided resource.
// Returns error if the resource is not a runtime.Object w/ metadata.
func (f *fakeOpa) keyFor(obj interface{}) (opaKey, error) {
	o, ok := obj.(runtime.Object)
	if !ok {
		return opaKey{}, fmt.Errorf("expected runtime.Object, got: %T", obj)
	}
	gvk := o.GetObjectKind().GroupVersionKind()
	k, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return opaKey{}, fmt.Errorf("extracting key: %v", err)
	}
	return opaKey{
		gvk: gvk,
		key: k,
	}, nil
}
func (f *fakeOpa) AddData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key, err := f.keyFor(data)
	if err != nil {
		return nil, err
	}

	if f.data == nil {
		f.data = make(map[opaKey]interface{})
	}

	f.data[key] = data
	return &constraintTypes.Responses{}, nil
}

func (f *fakeOpa) RemoveData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := data.(target.WipeData); ok {
		f.data = make(map[opaKey]interface{})
		return &constraintTypes.Responses{}, nil
	}

	key, err := f.keyFor(data)
	if err != nil {
		return nil, err
	}

	delete(f.data, key)
	return &constraintTypes.Responses{}, nil
}

// Contains returns true if all expected resources are in the cache.
func (f *fakeOpa) Contains(expected map[opaKey]interface{}) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for k := range expected {
		if _, ok := f.data[k]; !ok {
			return false
		}
	}
	return true
}

// HasGVK returns true if the cache has any data of the requested kind.
func (f *fakeOpa) HasGVK(gvk schema.GroupVersionKind) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for k := range f.data {
		if k.gvk == gvk {
			return true
		}
	}
	return false
}

// Len returns the number of items in the cache.
func (f *fakeOpa) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
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
					ExcludedNamespaces: []string{"kube-system"},
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

// This interface is getting used by tests to check the private objects of objectTracker
type testExpectations interface {
	ExpectedContains(gvk schema.GroupVersionKind, nsName types.NamespacedName) bool
}
