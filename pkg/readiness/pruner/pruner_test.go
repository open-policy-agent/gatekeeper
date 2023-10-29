package pruner

import (
	"context"
	"fmt"
	"testing"
	"time"

	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	timeout = 10 * time.Second
	tick    = 1 * time.Second
)

var cfg *rest.Config

var (
	syncsetGVK = syncsetv1alpha1.GroupVersion.WithKind("SyncSet")
	configGVK  = configv1alpha1.GroupVersion.WithKind("Config")

	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	podGVK       = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
)

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

type testOptions struct {
	addConstrollers       bool
	addExpectationsPruner bool
	testLister            readiness.Lister
}

type testResources struct {
	expectationsPruner *ExpectationsPruner
	manager            manager.Manager
	k8sClient          client.Client
}

func setupTest(ctx context.Context, t *testing.T, o testOptions) *testResources {
	t.Helper()

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	var tracker *readiness.Tracker
	var err error
	if o.testLister != nil {
		tracker = readiness.NewTracker(o.testLister, false, false, false)
		require.NoError(t, mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
			return tracker.Run(ctx)
		})), "adding tracker to manager")
	} else {
		tracker, err = readiness.SetupTrackerNoReadyz(mgr, false, false, false)
		require.NoError(t, err, "setting up tracker")
	}

	events := make(chan event.GenericEvent, 1024)
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events,
	)
	require.NoError(t, err, "creating registrar")

	cfClient := &fakes.FakeCfClient{}
	config := &cachemanager.Config{
		CfClient:         cfClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  process.Get(),
		Registrar:        reg,
		Reader:           c,
		GVKAggregator:    aggregator.NewGVKAggregator(),
	}
	require.NoError(t, err, "creating registrar")
	cm, err := cachemanager.NewCacheManager(config)
	require.NoError(t, err, "creating cachemanager")

	if !o.addConstrollers {
		// need to start the cachemanager if controllers are not started
		// since the cachemanager is started in the controllers code.
		require.NoError(t, mgr.Add(cm), "adding cachemanager as a runnable")
	} else {
		sw := watch.NewSwitch()
		mutationSystem := mutation.NewSystem(mutation.SystemOpts{})

		frameworksexternaldata.NewCache()
		opts := controller.Dependencies{
			CFClient:         testutils.SetupDataClient(t),
			WatchManger:      wm,
			ControllerSwitch: sw,
			Tracker:          tracker,
			ProcessExcluder:  process.Get(),
			MutationSystem:   mutationSystem,
			ExpansionSystem:  expansion.NewSystem(mutationSystem),
			ProviderCache:    frameworksexternaldata.NewCache(),
			CacheMgr:         cm,
			SyncEventsCh:     events,
		}
		require.NoError(t, controller.AddToManager(mgr, &opts), "registering controllers")
	}

	ep := &ExpectationsPruner{
		cacheMgr: cm,
		tracker:  tracker,
	}
	if o.addExpectationsPruner {
		require.NoError(t, mgr.Add(ep), "adding expectationspruner as a runnable")
	}

	return &testResources{expectationsPruner: ep, manager: mgr, k8sClient: c}
}

// Test_ExpectationsMgr_DeletedSyncSets tests scenarios in which SyncSet and Config resources
// get deleted after tracker expectations have been populated and we need to reconcile
// the GVKs that are in the data client (via the cachemaanger) and the GVKs that are expected
// by the Tracker.
func Test_ExpectationsMgr_DeletedSyncSets(t *testing.T) {
	tts := []struct {
		name             string
		fixturesPath     string
		syncsetsToDelete []string
		deleteConfig     string
		// not starting controllers approximates missing events in the informers cache
		startControllers bool
	}{
		{
			name:             "delete all syncsets",
			fixturesPath:     "testdata/syncsets-overlapping",
			syncsetsToDelete: []string{"syncset-1", "syncset-2", "syncset-3"},
		},
		{
			name:             "delete syncs and configs",
			fixturesPath:     "testdata/syncsets-config-disjoint",
			syncsetsToDelete: []string{"syncset-1"},
			deleteConfig:     "config",
		},
		{
			name:             "delete one syncset",
			fixturesPath:     "testdata/syncsets-resources",
			syncsetsToDelete: []string{"syncset-2"},
			startControllers: true,
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancelFunc := context.WithCancel(context.Background())

			require.NoError(t, testutils.ApplyFixtures(tt.fixturesPath, cfg), "applying base fixtures")

			testRes := setupTest(ctx, t, testOptions{addConstrollers: tt.startControllers})

			testutils.StartManager(ctx, t, testRes.manager)

			require.Eventually(t, func() bool {
				return testRes.expectationsPruner.tracker.Populated()
			}, timeout, tick, "waiting on tracker to populate")

			for _, name := range tt.syncsetsToDelete {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(syncsetGVK)
				u.SetName(name)

				require.NoError(t, testRes.k8sClient.Delete(ctx, u), fmt.Sprintf("deleting syncset %s", name))
			}
			if tt.deleteConfig != "" {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(configGVK)
				u.SetNamespace("gatekeeper-system")
				u.SetName(tt.deleteConfig)

				require.NoError(t, testRes.k8sClient.Delete(ctx, u), fmt.Sprintf("deleting config %s", tt.deleteConfig))
			}

			testRes.expectationsPruner.pruneNotWatchedGVKs()

			require.Eventually(t, func() bool {
				return testRes.expectationsPruner.tracker.Satisfied()
			}, timeout, tick, "waiting on tracker to get satisfied")

			cancelFunc()
		})
	}
}

// Test_ExpectationsMgr_missedInformers verifies that the pruner can handle a scenario
// where the readiness tracker's state will never match the informer cache events.
func Test_ExpectationsMgr_missedInformers(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// Set up one data store for readyTracker:
	// we will use a separate lister for the tracker from the mgr client and make
	// the contents of the readiness tracker be a superset of the contents of the mgr's client
	// the syncset will look like:
	// *fakes.SyncSetFor("syncset-a", []schema.GroupVersionKind{podGVK, configMapGVK}),
	testRes := setupTest(ctx, t, testOptions{testLister: makeTestLister(t), addExpectationsPruner: true, addConstrollers: true})

	// Set up another store for the controllers and watchManager
	syncsetA := fakes.SyncSetFor("syncset-a", []schema.GroupVersionKind{podGVK})
	require.NoError(t, testRes.k8sClient.Create(ctx, syncsetA))

	testutils.StartManager(ctx, t, testRes.manager)

	require.Eventually(t, func() bool {
		return testRes.expectationsPruner.tracker.SyncSourcesSatisfied()
	}, timeout, tick, "waiting on sync sources to get satisfied")

	// As configMapGVK is absent from this syncset-a, the CacheManager will never observe configMapGVK
	// being deleted and will never cancel the data expectation for configMapGVK.
	require.NoError(t, testRes.k8sClient.Delete(ctx, syncsetA))

	require.Eventually(t, func() bool {
		return testRes.expectationsPruner.tracker.Satisfied()
	}, timeout, tick, "waiting on tracker to get satisfied")

	cancelFunc()
}

func makeTestLister(t *testing.T) readiness.Lister {
	syncsetList := &syncsetv1alpha1.SyncSetList{}
	syncsetList.Items = []syncsetv1alpha1.SyncSet{
		*fakes.SyncSetFor("syncset-a", []schema.GroupVersionKind{podGVK, configMapGVK}),
	}

	podList := &unstructured.UnstructuredList{}
	podList.SetGroupVersionKind(schema.GroupVersionKind{
		Version: "v1",
		Kind:    "PodList",
	})
	podList.Items = []unstructured.Unstructured{
		*fakes.UnstructuredFor(podGVK, "", "pod1-name"),
	}

	configMapList := &unstructured.UnstructuredList{}
	configMapList.SetGroupVersionKind(schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMapList",
	})
	configMapList.Items = []unstructured.Unstructured{
		*fakes.UnstructuredFor(configMapGVK, "", "cm1-name"),
	}

	return fake.NewClientBuilder().WithLists(syncsetList, podList, configMapList).Build()
}
