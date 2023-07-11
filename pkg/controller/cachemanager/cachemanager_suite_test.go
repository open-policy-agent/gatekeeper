package cachemanager

import (
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/cachemanager/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

func makeCacheManagerForTest(t *testing.T, startCache, startManager bool) (*CacheManager, client.Client, context.Context) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	mgr, wm := testutils.SetupManager(t, cfg)

	c := testclient.NewRetryClient(mgr.GetClient())
	opaClient := &fakes.FakeOpa{}
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
	cacheManager, err := NewCacheManager(&Config{
		Opa:              opaClient,
		SyncMetricsCache: syncutil.NewMetricsCache(),
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		WatchedSet:       watch.NewSet(),
		Registrar:        w,
		Reader:           c,
	})
	require.NoError(t, err)

	if startCache {
		syncAdder := syncc.Adder{
			Events:       events,
			CacheManager: cacheManager,
		}
		require.NoError(t, syncAdder.Add(mgr), "registering sync controller")
		go func() {
			require.NoError(t, cacheManager.Start(ctx))
		}()

		t.Cleanup(func() {
			ctx.Done()
		})
	}

	if startManager {
		testutils.StartManager(ctx, t, mgr)
	}

	t.Cleanup(func() {
		cancelFunc()
	})
	return cacheManager, c, ctx
}

// makeUnitCacheManagerForTest creates a cache manager without starting the controller-runtime manager
// and without starting the cache manager background process. Note that this also means that the
// watch manager is not started and the sync controller is not started.
func makeUnitCacheManagerForTest(t *testing.T) (*CacheManager, context.Context) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)
	return cm, ctx
}

func newSyncExcluderFor(nsToExclude string) *process.Excluder {
	excluder := process.New()
	excluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{wildcard.Wildcard(nsToExclude)},
			Processes:          []string{"sync"},
		},
		// exclude kube-system by default to prevent noise
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"sync"},
		},
	})

	return excluder
}
