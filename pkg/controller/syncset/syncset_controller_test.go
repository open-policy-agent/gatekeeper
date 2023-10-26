package syncset

import (
	"fmt"
	"testing"
	"time"

	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	nsGVK        = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	podGVK       = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
)

const (
	timeout = time.Second * 10
	tick    = time.Second * 2
)

// Test_ReconcileSyncSet_wConfigController verifies that SyncSet and Config resources
// can get reconciled and their respective specs are added to the data client.
func Test_ReconcileSyncSet_wConfigController(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	testRes := setupTest(ctx, t)

	configMap := fakes.UnstructuredFor(configMapGVK, "", "cm1-name")
	pod := fakes.UnstructuredFor(podGVK, "", "pod1-name")

	// the sync controller populates the cache based on replay events from the watchmanager
	syncAdder := syncc.Adder{CacheManager: testRes.cacheMgr, Events: testRes.events}
	require.NoError(t, syncAdder.Add(*testRes.mgr), "adding sync reconciler to mgr")

	configAdder := config.Adder{
		CacheManager:     testRes.cacheMgr,
		ControllerSwitch: testRes.cs,
		Tracker:          testRes.tracker,
	}
	require.NoError(t, configAdder.Add(*testRes.mgr), "adding config reconciler to mgr")

	testutils.StartManager(ctx, t, *testRes.mgr)

	require.NoError(t, testRes.k8sclient.Create(ctx, configMap), fmt.Sprintf("creating ConfigMap %s", "cm1-mame"))
	require.NoError(t, testRes.k8sclient.Create(ctx, pod), fmt.Sprintf("creating Pod %s", "pod1-name"))

	tts := []struct {
		name         string
		syncSources  []client.Object
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "config and 1 sync",
			syncSources: []client.Object{
				fakes.ConfigFor([]schema.GroupVersionKind{configMapGVK, nsGVK}),
				fakes.SyncSetFor("syncset1", []schema.GroupVersionKind{podGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK, nsGVK},
		},
		{
			name: "config only",
			syncSources: []client.Object{
				fakes.ConfigFor([]schema.GroupVersionKind{configMapGVK, nsGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, nsGVK},
		},
		{
			name: "syncset only",
			syncSources: []client.Object{
				fakes.SyncSetFor("syncset1", []schema.GroupVersionKind{configMapGVK, nsGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, nsGVK},
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			for _, o := range tt.syncSources {
				require.NoError(t, testRes.k8sclient.Create(ctx, o))
			}

			assert.Eventually(t, func() bool {
				for _, gvk := range tt.expectedGVKs {
					if !testRes.cfClient.HasGVK(gvk) {
						return false
					}
				}
				return true
			}, timeout, tick)

			// empty the cache to not leak state between tests
			for _, o := range tt.syncSources {
				require.NoError(t, testRes.k8sclient.Delete(ctx, o))
			}
			require.Eventually(t, func() bool {
				return testRes.cfClient.Len() == 0
			}, timeout, tick, "could not cleanup")
		})
	}
}

type testResources struct {
	mgr       *manager.Manager
	cacheMgr  *cm.CacheManager
	k8sclient *testclient.RetryClient
	wm        *watch.Manager
	cfClient  *fakes.FakeCfClient
	events    chan event.GenericEvent
	cs        *watch.ControllerSwitch
	tracker   *readiness.Tracker
}

func setupTest(ctx context.Context, t *testing.T) testResources {
	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	testRes := testResources{}

	cs := watch.NewSwitch()
	testRes.cs = cs
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	require.NoError(t, err)
	testRes.tracker = tracker

	processExcluder := process.Get()
	events := make(chan event.GenericEvent, 1024)
	testRes.events = events
	syncMetricsCache := syncutil.NewMetricsCache()
	w, err := wm.NewRegistrar(
		cm.RegistrarName,
		events)
	require.NoError(t, err)

	cfClient := &fakes.FakeCfClient{}
	testRes.cfClient = cfClient
	cm, err := cm.NewCacheManager(&cm.Config{CfClient: cfClient, SyncMetricsCache: syncMetricsCache, Tracker: tracker, ProcessExcluder: processExcluder, Registrar: w, Reader: c})
	require.NoError(t, err)
	go func() {
		assert.NoError(t, cm.Start(ctx))
	}()

	testRes.mgr = &mgr
	testRes.cacheMgr = cm
	testRes.k8sclient = c
	testRes.wm = wm

	rec, err := newReconciler(mgr, cm, cs, tracker)
	require.NoError(t, err)

	require.NoError(t, add(mgr, rec))

	return testRes
}
