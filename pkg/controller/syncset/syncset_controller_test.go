package syncset

import (
	"fmt"
	"sync"
	"testing"
	"time"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	syncc "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

	tr := setupTest(ctx, t, setupOpts{useFakeClient: true})

	configMap := testutils.UnstructuredFor(configMapGVK, "", "cm1-name")
	pod := testutils.UnstructuredFor(podGVK, "", "pod1-name")

	// installing sync controller is needed for data to actually be populated in the cache
	syncAdder := syncc.Adder{CacheManager: tr.cacheMgr, Events: tr.events}
	require.NoError(t, syncAdder.Add(*tr.mgr), "adding sync reconciler to mgr")

	configAdder := config.Adder{
		CacheManager:     tr.cacheMgr,
		ControllerSwitch: tr.cs,
		Tracker:          tr.tracker,
	}
	require.NoError(t, configAdder.Add(*tr.mgr), "adding config reconciler to mgr")

	testutils.StartManager(ctx, t, *tr.mgr)

	require.NoError(t, tr.c.Create(ctx, configMap), fmt.Sprintf("creating ConfigMap %s", "cm1-mame"))
	require.NoError(t, tr.c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", "pod1-name"))

	tts := []struct {
		name         string
		syncSources  []client.Object
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "config and 1 sync",
			syncSources: []client.Object{
				testutils.ConfigFor([]schema.GroupVersionKind{configMapGVK, nsGVK}),
				testutils.SyncSetFor("syncset1", []schema.GroupVersionKind{podGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK, nsGVK},
		},
		{
			name: "config and multiple sync",
			syncSources: []client.Object{
				testutils.ConfigFor([]schema.GroupVersionKind{configMapGVK, nsGVK}),
				testutils.SyncSetFor("syncset1", []schema.GroupVersionKind{podGVK}),
				testutils.SyncSetFor("syncset2", []schema.GroupVersionKind{configMapGVK}),
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			for _, o := range tt.syncSources {
				require.NoError(t, tr.c.Create(ctx, o))
			}

			assert.Eventually(t, func() bool {
				for _, gvk := range tt.expectedGVKs {
					if !tr.cfClient.HasGVK(gvk) {
						return false
					}
				}
				return true
			}, timeout, tick)

			// reset the sync instances for a clean slate between test cases
			for _, o := range tt.syncSources {
				require.NoError(t, tr.c.Delete(ctx, o))
			}

			require.Eventually(t, func() bool {
				return tr.cfClient.Len() == 0
			}, timeout, tick, "could not cleanup")
		})
	}
}

type testResources struct {
	mgr      *manager.Manager
	requests *sync.Map
	cacheMgr *cm.CacheManager
	c        *testclient.RetryClient
	wm       *watch.Manager
	cfClient *fakes.FakeCfClient
	events   chan event.GenericEvent
	cs       *watch.ControllerSwitch
	tracker  *readiness.Tracker
}

type setupOpts struct {
	wrapReconciler bool
	useFakeClient  bool
}

func setupTest(ctx context.Context, t *testing.T, opts setupOpts) testResources {
	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	tr := testResources{}
	var dataClient cm.CFDataClient
	if opts.useFakeClient {
		cfClient := &fakes.FakeCfClient{}
		dataClient = cfClient
		tr.cfClient = cfClient
	} else {
		driver, err := rego.New()
		require.NoError(t, err, "unable to set up driver")

		dataClient, err = constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
		require.NoError(t, err, "unable to set up data client")
	}

	cs := watch.NewSwitch()
	tr.cs = cs
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	require.NoError(t, err)
	tr.tracker = tracker

	processExcluder := process.Get()
	events := make(chan event.GenericEvent, 1024)
	tr.events = events
	syncMetricsCache := syncutil.NewMetricsCache()
	w, err := wm.NewRegistrar(
		cm.RegistrarName,
		events)
	require.NoError(t, err)

	cm, err := cm.NewCacheManager(&cm.Config{CfClient: dataClient, SyncMetricsCache: syncMetricsCache, Tracker: tracker, ProcessExcluder: processExcluder, Registrar: w, Reader: c})
	require.NoError(t, err)
	go func() {
		assert.NoError(t, cm.Start(ctx))
	}()

	tr.mgr = &mgr
	tr.cacheMgr = cm
	tr.c = c
	tr.wm = wm

	rec, err := newReconciler(mgr, cm, cs, tracker)
	require.NoError(t, err)

	if opts.wrapReconciler {
		recFn, requests := testutils.SetupTestReconcile(rec)
		require.NoError(t, add(mgr, recFn))
		tr.requests = requests
	} else {
		require.NoError(t, add(mgr, rec))
	}

	return tr
}

func Test_ReconcileSyncSet_Reconcile(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	tr := setupTest(ctx, t, setupOpts{wrapReconciler: true})

	testutils.StartManager(ctx, t, *tr.mgr)

	syncset1 := &syncsetv1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "syncset",
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: []syncsetv1alpha1.GVKEntry{syncsetv1alpha1.GVKEntry(podGVK)},
		},
	}
	require.NoError(t, tr.c.Create(ctx, syncset1))

	require.Eventually(t, func() bool {
		_, ok := tr.requests.Load(reconcile.Request{NamespacedName: types.NamespacedName{Name: "syncset"}})

		return ok
	}, timeout, tick, "waiting on syncset request to be received")

	require.Eventually(t, func() bool {
		return len(tr.wm.GetManagedGVK()) == 1
	}, timeout, tick, "check watched gvks are populated")

	gvks := tr.wm.GetManagedGVK()
	wantGVKs := []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Pod"},
	}
	require.ElementsMatch(t, wantGVKs, gvks)

	// now delete the sync source and expect no longer watched gvks
	require.NoError(t, tr.c.Delete(ctx, syncset1))
	require.Eventually(t, func() bool {
		return len(tr.wm.GetManagedGVK()) == 0
	}, timeout, tick, "check watched gvks are deleted")
	require.ElementsMatch(t, []schema.GroupVersionKind{}, tr.wm.GetManagedGVK())
}
