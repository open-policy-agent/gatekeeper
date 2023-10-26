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
	startControllers bool
	testLister       readiness.Lister
}

func setupTest(ctx context.Context, t *testing.T, o testOptions) (*ExpectationsPruner, client.Client) {
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

	if !o.startControllers {
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

	testutils.StartManager(ctx, t, mgr)

	return &ExpectationsPruner{
		cacheMgr: cm,
		tracker:  tracker,
	}, c
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

			em, c := setupTest(ctx, t, testOptions{startControllers: tt.startControllers})

			require.Eventually(t, func() bool {
				return em.tracker.Populated()
			}, timeout, tick, "waiting on tracker to populate")

			for _, name := range tt.syncsetsToDelete {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(syncsetGVK)
				u.SetName(name)

				require.NoError(t, c.Delete(ctx, u), fmt.Sprintf("deleting syncset %s", name))
			}
			if tt.deleteConfig != "" {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(configGVK)
				u.SetNamespace("gatekeeper-system")
				u.SetName(tt.deleteConfig)

				require.NoError(t, c.Delete(ctx, u), fmt.Sprintf("deleting config %s", tt.deleteConfig))
			}

			em.pruneNotWatchedGVKs()

			require.Eventually(t, func() bool {
				return em.tracker.Satisfied()
			}, timeout, tick, "waiting on tracker to get satisfied")

			cancelFunc()
		})
	}
}

// Test_ExpectationsMgr_missedInformers verifies that the pruner can handle a scenario
// where the readiness tracker's state will never match the informer cache events.
func Test_ExpectationsMgr_missedInformers(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// because we will use a separate lister for the tracker from the mgr client
	// the contents of the readiness tracker will be superset of the contents of the mgr's client
	tl := fakes.NewTestLister(
		fakes.WithSyncSets([]*syncsetv1alpha1.SyncSet{
			fakes.SyncSetFor("syncset-1", []schema.GroupVersionKind{podGVK, configMapGVK}),
		}),
	)
	em, _ := setupTest(ctx, t, testOptions{testLister: tl})

	require.Eventually(t, func() bool {
		return em.tracker.SyncSourcesSatisfied()
	}, timeout, tick, "waiting on sync sources to get satisfied")

	em.pruneNotWatchedGVKs()

	require.Eventually(t, func() bool {
		return em.tracker.Satisfied()
	}, timeout, tick, "waiting on tracker to get satisfied")

	cancelFunc()
}
