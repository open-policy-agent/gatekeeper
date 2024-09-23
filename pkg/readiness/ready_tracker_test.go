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

package readiness_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	ttimeout = 20 * time.Second
	ttick    = 1 * time.Second
)

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	logger := zap.New(zap.UseDevMode(true), zap.WriteTo(testutils.NewTestWriter(t)))
	ctrl.SetLogger(logger)
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		HealthProbeBindAddress: "127.0.0.1:29090",
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Logger:         logger,
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

func setupDataClient(t *testing.T) *constraintclient.Client {
	driver, err := rego.New(rego.Tracing(false))
	if err != nil {
		t.Fatalf("setting up Driver: %v", err)
	}

	client, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("setting up constraint framework client: %v", err)
	}
	return client
}

func setupController(
	mgr manager.Manager,
	wm *watch.Manager,
	cfClient *constraintclient.Client,
	mutationSystem *mutation.System,
	expansionSystem *expansion.System,
	providerCache *frameworksexternaldata.ProviderCache,
) error {
	*expansion.ExpansionEnabled = expansionSystem != nil

	tracker, err := readiness.SetupTracker(mgr, mutationSystem != nil, providerCache != nil, expansionSystem != nil)
	if err != nil {
		return fmt.Errorf("setting up tracker: %w", err)
	}

	sw := watch.NewSwitch()

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	processExcluder := process.Get()

	events := make(chan event.GenericEvent, 1024)
	syncMetricsCache := syncutil.NewMetricsCache()
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events)
	if err != nil {
		return fmt.Errorf("setting up watch manager: %w", err)
	}
	cacheManager, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           mgr.GetCache(),
	})
	if err != nil {
		return fmt.Errorf("setting up cache manager: %w", err)
	}

	// Setup all Controllers
	opts := controller.Dependencies{
		CFClient:         cfClient,
		WatchManger:      wm,
		ControllerSwitch: sw,
		Tracker:          tracker,
		GetPod:           func(_ context.Context) (*corev1.Pod, error) { return pod, nil },
		ProcessExcluder:  processExcluder,
		MutationSystem:   mutationSystem,
		ExpansionSystem:  expansionSystem,
		ProviderCache:    providerCache,
		CacheMgr:         cacheManager,
		SyncEventsCh:     events,
	}
	if err := controller.AddToManager(mgr, &opts); err != nil {
		return fmt.Errorf("registering controllers: %w", err)
	}
	return nil
}

func Test_AssignMetadata(t *testing.T) {
	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are setup.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})
	expansionSystem := expansion.NewSystem(mutationSystem)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutationSystem, expansionSystem, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g := gomega.NewWithT(t)
	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 30*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the AssignMetadata is present in the cache
	for _, am := range testAssignMetadata {
		id := mutationtypes.MakeID(am)
		expectedMutator := mutationSystem.Get(id)

		if expectedMutator == nil {
			t.Errorf("got Get(%v) = nil, want non-nil", id)
		}
	}
}

func Test_ModifySet(t *testing.T) {
	g := gomega.NewWithT(t)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are set up.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})
	expansionSystem := expansion.NewSystem(mutationSystem)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutationSystem, expansionSystem, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the ModifySet is present in the cache
	for _, am := range testModifySet {
		id := mutationtypes.MakeID(am)
		expectedMutator := mutationSystem.Get(id)
		if expectedMutator == nil {
			t.Fatal("want expectedMutator != nil but got nil")
		}
	}
}

func Test_AssignImage(t *testing.T) {
	g := gomega.NewWithT(t)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are set up.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})
	expansionSystem := expansion.NewSystem(mutationSystem)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutationSystem, expansionSystem, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the AssignImage is present in the cache
	for _, am := range testAssignImage {
		id := mutationtypes.MakeID(am)
		expectedMutator := mutationSystem.Get(id)
		if expectedMutator == nil {
			t.Fatal("want expectedMutator != nil but got nil")
		}
	}
}

func Test_Assign(t *testing.T) {
	g := gomega.NewWithT(t)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are setup.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})
	expansionSystem := expansion.NewSystem(mutationSystem)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutationSystem, expansionSystem, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the Assign is present in the cache
	for _, am := range testAssign {
		id := mutationtypes.MakeID(am)
		expectedMutator := mutationSystem.Get(id)
		if expectedMutator == nil {
			t.Fatal("want expectedMutator != nil but got nil")
		}
	}
}

func Test_ExpansionTemplate(t *testing.T) {
	g := gomega.NewWithT(t)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are setup.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})
	expansionSystem := expansion.NewSystem(mutationSystem)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutationSystem, expansionSystem, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the ExpansionTemplate is registered by expanding a demo deployment
	// and checking that the resulting Pod is non-nil
	deployment := makeDeployment("demo-deployment")
	o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		panic(fmt.Errorf("error converting deployment to unstructured: %w", err))
	}
	u := unstructured.Unstructured{Object: o}
	m := mutationtypes.Mutable{
		Object:    &u,
		Namespace: testNS,
		Username:  "",
		Source:    "All",
	}
	res, err := expansionSystem.Expand(&m)
	if err != nil {
		panic(fmt.Errorf("error expanding: %w", err))
	}
	if len(res) != 1 {
		t.Fatal("expected generator to expand into 1 pod, but got 0 resultants")
	}
}

func Test_Provider(t *testing.T) {
	g := gomega.NewWithT(t)

	providerCache := frameworksexternaldata.NewCache()

	err := os.Setenv("POD_NAME", "no-pod")
	if err != nil {
		t.Fatal(err)
	}
	// Apply fixtures *before* the controllers are setup.
	err = applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)

	if err := setupController(mgr,
		wm,
		cfClient,
		mutation.NewSystem(mutation.SystemOpts{}),
		nil,
		providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify that the Provider is present in the cache
	for _, tp := range testProvider {
		instance, err := providerCache.Get(tp.Name)
		if err != nil {
			t.Fatal(err)
		}

		want := externaldataUnversioned.ProviderSpec{
			URL:      "https://demo",
			Timeout:  1,
			CABundle: util.ValidCABundle,
		}
		if diff := cmp.Diff(want, instance.Spec); diff != "" {
			t.Fatal(diff)
		}
	}
}

// Test_Tracker verifies that once an initial set of fixtures are loaded into OPA,
// the readiness probe reflects that Gatekeeper is ready to enforce policy. Adding
// additional constraints afterwards will not change the readiness state.
//
// Fixtures are loaded from testdata/ and testdata/post.
// CRDs are loaded from testdata/crds (see TestMain).
// Corresponding expectations are in testdata_test.go.
func Test_Tracker(t *testing.T) {
	g := gomega.NewWithT(t)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	// Apply fixtures *before* the controllers are setup.
	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	// Wire up the rest.
	mgr, wm := setupManager(t)
	cfClient := setupDataClient(t)
	providerCache := frameworksexternaldata.NewCache()

	if err := setupController(mgr, wm, cfClient, mutation.NewSystem(mutation.SystemOpts{}), nil, providerCache); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	if err := createGatekeeperNamespace(mgr.GetConfig()); err != nil {
		t.Fatalf("want createGatekeeperNamespace(mgr.GetConfig()) error = nil, got %v", err)
	}

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 20*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify cache (tracks testdata fixtures)
	for _, ct := range testTemplates {
		_, err := cfClient.GetTemplate(ct)
		if err != nil {
			t.Fatalf("checking cache for template: %v", err)
		}
	}
	for _, c := range testConstraints {
		_, err := cfClient.GetConstraint(c)
		if err != nil {
			t.Fatalf("checking cache for constraint: %v", err)
		}
	}
	// TODO: Verify data if we add the corresponding API to cf.Client.
	// for _, d := range testData {
	// 	_, err := cfClient.GetData(ctx, c)
	// 	if err != nil {
	// t.Fatalf("checking cache for constraint: %v", err)
	// }

	// Add additional templates/constraints and verify that we remain satisfied
	err = applyFixtures("testdata/post")
	if err != nil {
		t.Fatalf("applying post fixtures: %v", err)
	}

	g.Eventually(func() (bool, error) {
		// Verify cache (tracks testdata/post fixtures)
		for _, ct := range postTemplates {
			_, err := cfClient.GetTemplate(ct)
			if err != nil {
				return false, err
			}
		}
		for _, c := range postConstraints {
			_, err := cfClient.GetConstraint(c)
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}, 20*time.Second, 100*time.Millisecond).Should(gomega.BeTrue(), "verifying cache for post-fixtures")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	t.Cleanup(cancel)

	ready, err := probeIsReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("probe should become ready after adding additional constraints")
	}
}

// Verifies additional scenarios to the base "testdata/" fixtures, such as
// invalid Config resources or overlapping SyncSets, which we want to
// make sure the ReadyTracker can handle gracefully.
func Test_Tracker_SyncSourceEdgeCases(t *testing.T) {
	tts := []struct {
		name         string
		fixturesPath string
	}{
		{
			name:         "overlapping syncsets",
			fixturesPath: "testdata/syncset",
		},
		{
			// bad gvk in Config doesn't halt readiness
			name:         "bad gvk",
			fixturesPath: "testdata/config/bad-gvk",
		},
		{
			// repeating gvk in Config doesn't halt readiness
			name:         "repeating gvk",
			fixturesPath: "testdata/config/repeating-gvk",
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			testutils.Setenv(t, "POD_NAME", "no-pod")
			require.NoError(t, applyFixtures("testdata"), "base fixtures")
			require.NoError(t, applyFixtures(tt.fixturesPath), fmt.Sprintf("test fixtures: %s", tt.fixturesPath))

			mgr, wm := setupManager(t)
			cfClient := testutils.SetupDataClient(t)
			providerCache := frameworksexternaldata.NewCache()

			require.NoError(t, setupController(mgr, wm, cfClient, mutation.NewSystem(mutation.SystemOpts{}), nil, providerCache))

			ctx, cancelFunc := context.WithCancel(context.Background())
			testutils.StartManager(ctx, t, mgr)

			require.Eventually(t, func() bool {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()

				ready, err := probeIsReady(ctx)
				assert.NoError(t, err, "error while waiting for probe to be ready")

				return ready
			}, ttimeout, ttick, "tracker not healthy")

			cancelFunc()
		})
	}
}

// Test_CollectDeleted adds resources and starts the readiness tracker, then
// deletes the expected resources and ensures that the trackers watching these
// resources correctly identify the deletions and remove the corresponding expectations.
// Note that the main controllers are not running in order to target testing to the
// readiness tracker.
func Test_CollectDeleted(t *testing.T) {
	type test struct {
		description string
		gvk         schema.GroupVersionKind
		tracker     *readiness.Expectations
	}

	g := gomega.NewWithT(t)

	err := applyFixtures("testdata")
	if err != nil {
		t.Fatalf("applying fixtures: %v", err)
	}

	mgr, _ := setupManager(t)

	// Setup tracker with namespaced client to avoid "noise" (control-plane-managed configmaps) from kube-system
	lister := namespacedLister{
		lister:    mgr.GetAPIReader(),
		namespace: "gatekeeper-system",
	}
	tracker := readiness.NewTracker(lister, false, false, false)
	err = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return tracker.Run(ctx)
	}))
	if err != nil {
		t.Fatalf("setting up tracker: %v", err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	client := mgr.GetClient()

	if tracker.Satisfied() {
		t.Fatal("checking the overall tracker is unsatisfied")
	}

	// set up expected GVKs for tests
	cgvk := schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "K8sRequiredLabels",
	}

	cm := &corev1.ConfigMap{}
	cmgvk, err := apiutil.GVKForObject(cm, mgr.GetScheme())
	if err != nil {
		t.Fatalf("retrieving ConfigMap GVK: %v", err)
	}
	cmtracker := tracker.ForData(cmgvk)

	ct := &v1beta1.ConstraintTemplate{}
	ctgvk, err := apiutil.GVKForObject(ct, mgr.GetScheme())
	if err != nil {
		t.Fatalf("retrieving ConstraintTemplate GVK: %v", err)
	}
	config := &configv1alpha1.Config{}
	configGvk, err := apiutil.GVKForObject(config, mgr.GetScheme())
	require.NoError(t, err)

	syncset := &syncsetv1alpha1.SyncSet{}
	syncsetGvk, err := apiutil.GVKForObject(syncset, mgr.GetScheme())
	require.NoError(t, err)

	// note: state can leak between these test cases because we do not reset the environment
	// between them to keep the test short. Trackers are mostly independent per GVK.
	tests := []test{
		{description: "constraints", gvk: cgvk},
		{description: "data (configmaps)", gvk: cmgvk, tracker: &cmtracker},
		{description: "templates", gvk: ctgvk},
		// (the objects identified in a Config's syncOnly are Expected, tested in data case above)
		{description: "config", gvk: configGvk},
		{description: "syncset", gvk: syncsetGvk},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var tt readiness.Expectations
			if tc.tracker != nil {
				tt = *tc.tracker
			} else {
				tt = tracker.For(tc.gvk)
			}

			g.Eventually(func() (bool, error) {
				return tt.Populated() && !tt.Satisfied(), nil
			}, 20*time.Second, 1*time.Second).
				Should(gomega.BeTrue(), "checking the tracker is tracking %s correctly")

			ul := &unstructured.UnstructuredList{}
			ul.SetGroupVersionKind(tc.gvk)
			err = lister.List(ctx, ul)
			if err != nil {
				t.Fatalf("deleting all %s", tc.description)
			}
			if len(ul.Items) == 0 {
				t.Fatal("want items to be nonempty")
			}

			for index := range ul.Items {
				err = client.Delete(ctx, &ul.Items[index])
				if err != nil {
					t.Fatalf("deleting %s %s", tc.description, ul.Items[index].GetName())
				}
			}

			g.Eventually(func() (bool, error) {
				return tt.Satisfied(), nil
			}, 20*time.Second, 1*time.Second).
				Should(gomega.BeTrue(), "checking the tracker collects deletes of %s")
		})
	}
}

// probeIsReady checks whether expectations have been satisfied (via the readiness probe).
func probeIsReady(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:29090/readyz", http.NoBody)
	if err != nil {
		return false, fmt.Errorf("constructing http request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return false, err
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 400, nil
}

// namespacedLister scopes a lister to a particular namespace.
type namespacedLister struct {
	namespace string
	lister    readiness.Lister
}

func (n namespacedLister) List(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
	if n.namespace != "" {
		opts = append(opts, ctrlclient.InNamespace(n.namespace))
	}
	return n.lister.List(ctx, list, opts...)
}
