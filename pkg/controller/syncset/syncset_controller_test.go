package syncset

import (
	"context"
	"fmt"
	"testing"
	"time"

	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
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

// Test_ReconcileSyncSet verifies that SyncSet resources
// can get reconciled and their respective specs are added to the data client.
func Test_ReconcileSyncSet(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	testRes := setupTest(ctx, t)

	configMap := fakes.UnstructuredFor(configMapGVK, "", "cm1-name")
	pod := fakes.UnstructuredFor(podGVK, "", "pod1-name")

	// the sync controller populates the cache based on replay events from the watchmanager
	syncAdder := syncc.Adder{CacheManager: testRes.cacheMgr, Events: testRes.events}
	require.NoError(t, syncAdder.Add(*testRes.mgr), "adding sync reconciler to mgr")

	testutils.StartManager(ctx, t, *testRes.mgr)

	require.NoError(t, testRes.k8sclient.Create(ctx, configMap), fmt.Sprintf("creating ConfigMap %s", "cm1-mame"))
	require.NoError(t, testRes.k8sclient.Create(ctx, pod), fmt.Sprintf("creating Pod %s", "pod1-name"))

	tts := []struct {
		name         string
		syncSources  []*syncsetv1alpha1.SyncSet
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "SyncSet includes new GVKs",
			syncSources: []*syncsetv1alpha1.SyncSet{
				fakes.SyncSetFor("syncset1", []schema.GroupVersionKind{configMapGVK, nsGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, nsGVK},
		},
		{
			name: "New SyncSet generation has one less GVK than previous generation",
			syncSources: []*syncsetv1alpha1.SyncSet{
				fakes.SyncSetFor("syncset1", []schema.GroupVersionKind{configMapGVK, nsGVK}),
				fakes.SyncSetFor("syncset1", []schema.GroupVersionKind{nsGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{nsGVK},
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			created := map[string]struct{}{}
			for _, syncSource := range tt.syncSources {
				if _, ok := created[syncSource.GetName()]; ok {
					curObj, ok := syncSource.DeepCopyObject().(client.Object)
					require.True(t, ok)
					// eventually we should find the object
					require.Eventually(t, func() bool {
						return testRes.k8sclient.Get(ctx, client.ObjectKeyFromObject(curObj), curObj) == nil
					}, timeout, tick, fmt.Sprintf("getting %s", syncSource.GetName()))

					syncSource.SetResourceVersion(curObj.GetResourceVersion())
					require.NoError(t, testRes.k8sclient.Update(ctx, syncSource), fmt.Sprintf("updating %s", syncSource.GetName()))
				} else {
					require.NoError(t, testRes.k8sclient.Create(ctx, syncSource), fmt.Sprintf("creating %s", syncSource.GetName()))
					created[syncSource.GetName()] = struct{}{}
				}
			}

			assert.Eventually(t, func() bool {
				return testRes.cfClient.ContainsGVKs(tt.expectedGVKs)
			}, timeout, tick)

			// empty the cache to not leak state between tests
			deleted := map[string]struct{}{}
			for _, o := range tt.syncSources {
				if _, ok := deleted[o.GetName()]; ok {
					continue
				}
				require.NoError(t, testRes.k8sclient.Delete(ctx, o))
				deleted[o.GetName()] = struct{}{}
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
	tracker   *readiness.Tracker
}

func setupTest(ctx context.Context, t *testing.T) testResources {
	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	testRes := testResources{}

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

	rec, err := newReconciler(mgr, cm, tracker)
	require.NoError(t, err)

	require.NoError(t, add(mgr, rec))

	return testRes
}
