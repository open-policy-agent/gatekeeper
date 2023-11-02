package pruner

import (
	"context"
	"testing"
	"time"

	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
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
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	podGVK       = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
)

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

type testResources struct {
	expectationsPruner *ExpectationsPruner
	manager            manager.Manager
	k8sClient          client.Client
}

func setupTest(ctx context.Context, t *testing.T, readyTrackerClient readiness.Lister) *testResources {
	t.Helper()

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	tracker := readiness.NewTracker(readyTrackerClient, false, false, false)
	require.NoError(t, mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return tracker.Run(ctx)
	})), "adding tracker to manager")

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

	ep := &ExpectationsPruner{
		cacheMgr: cm,
		tracker:  tracker,
	}
	require.NoError(t, mgr.Add(ep), "adding expectationspruner as a runnable")

	return &testResources{expectationsPruner: ep, manager: mgr, k8sClient: c}
}

// Test_ExpectationsPruner_missedInformers verifies that the pruner can handle a scenario
// where the readiness tracker's state will never match the informer cache events.
func Test_ExpectationsPruner_missedInformers(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())

	// Set up one data store for readyTracker:
	// we will use a separate lister for the tracker from the mgr client and make
	// the contents of the readiness tracker be a superset of the contents of the mgr's client
	lister := fake.NewClientBuilder().WithRuntimeObjects(
		fakes.SyncSetFor("syncset-a", []schema.GroupVersionKind{podGVK, configMapGVK}),
		fakes.UnstructuredFor(podGVK, "", "pod1-name"),
		fakes.UnstructuredFor(configMapGVK, "", "cm1-name"),
	).Build()
	testRes := setupTest(ctx, t, lister)

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
