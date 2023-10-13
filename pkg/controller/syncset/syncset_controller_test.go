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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	configMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	nsGVK        = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	podGVK       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
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

	tr := setupTest(ctx, t, false, true)
	mgr := *tr.mgr
	c := tr.c
	cfClient := tr.cfClient

	instanceConfig := testutils.ConfigFor([]schema.GroupVersionKind{})
	instanceSyncSet1 := &syncsetv1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "syncset1",
		},
	}
	instanceSyncSet2 := &syncsetv1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "syncset2",
		},
	}
	configMap := testutils.UnstructuredFor(configMapGVK, "", "cm1-name")
	pod := testutils.UnstructuredFor(podGVK, "", "pod1-name")

	// for sync controller
	syncAdder := syncc.Adder{CacheManager: tr.cacheMgr, Events: tr.events}
	require.NoError(t, syncAdder.Add(mgr), "adding sync reconciler to mgr")

	// now for config controller
	configAdder := config.Adder{
		CacheManager:     tr.cacheMgr,
		ControllerSwitch: tr.cs,
		Tracker:          tr.tracker,
	}
	require.NoError(t, configAdder.Add(mgr), "adding config reconciler to mgr")

	testutils.StartManager(ctx, t, mgr)

	require.NoError(t, c.Create(ctx, configMap), fmt.Sprintf("creating ConfigMap %s", "cm1-mame"))
	require.NoError(t, c.Create(ctx, pod), fmt.Sprintf("creating Pod %s", "pod1-name"))

	tts := []struct {
		name         string
		setup        func(t *testing.T)
		cleanup      func(t *testing.T)
		expectedGVKs []schema.GroupVersionKind
	}{
		{
			name: "config and 1 sync",
			setup: func(t *testing.T) {
				t.Helper()

				instanceConfig := testutils.ConfigFor([]schema.GroupVersionKind{configMapGVK, nsGVK})
				instanceSyncSet := &syncsetv1alpha1.SyncSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "syncset1",
					},
					Spec: syncsetv1alpha1.SyncSetSpec{
						GVKs: []syncsetv1alpha1.GVKEntry{
							syncsetv1alpha1.GVKEntry(podGVK),
						},
					},
				}

				require.NoError(t, c.Create(ctx, instanceConfig))
				require.NoError(t, c.Create(ctx, instanceSyncSet))
			},
			cleanup: func(t *testing.T) {
				t.Helper()

				// reset the sync instances
				require.NoError(t, c.Delete(ctx, instanceConfig))
				require.NoError(t, c.Delete(ctx, instanceSyncSet1))
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK, nsGVK},
		},
		{
			name: "config and multiple sync",
			setup: func(t *testing.T) {
				t.Helper()

				instanceConfig := testutils.ConfigFor([]schema.GroupVersionKind{configMapGVK})
				instanceSyncSet1 = &syncsetv1alpha1.SyncSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "syncset1",
					},
					Spec: syncsetv1alpha1.SyncSetSpec{
						GVKs: []syncsetv1alpha1.GVKEntry{
							syncsetv1alpha1.GVKEntry(podGVK),
						},
					},
				}
				instanceSyncSet2 = &syncsetv1alpha1.SyncSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "syncset2",
					},
					Spec: syncsetv1alpha1.SyncSetSpec{
						GVKs: []syncsetv1alpha1.GVKEntry{
							syncsetv1alpha1.GVKEntry(configMapGVK),
						},
					},
				}

				require.NoError(t, c.Create(ctx, instanceConfig))
				require.NoError(t, c.Create(ctx, instanceSyncSet1))
				require.NoError(t, c.Create(ctx, instanceSyncSet2))
			},
			cleanup: func(t *testing.T) {
				t.Helper()

				// reset the sync instances
				require.NoError(t, c.Delete(ctx, instanceConfig))
				require.NoError(t, c.Delete(ctx, instanceSyncSet1))
				require.NoError(t, c.Delete(ctx, instanceSyncSet2))
			},
			expectedGVKs: []schema.GroupVersionKind{configMapGVK, podGVK},
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			assert.Eventually(t, expectedCheck(cfClient, tt.expectedGVKs), timeout, tick)

			if tt.cleanup != nil {
				tt.cleanup(t)

				require.Eventually(t, func() bool {
					return cfClient.Len() == 0
				}, timeout, tick, "could not cleanup")
			}
		})
	}
}

func expectedCheck(cfClient *fakes.FakeCfClient, expected []schema.GroupVersionKind) func() bool {
	return func() bool {
		for _, gvk := range expected {
			if !cfClient.HasGVK(gvk) {
				return false
			}
		}
		return true
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

func setupTest(ctx context.Context, t *testing.T, wrapReconciler bool, useFakeClient bool) testResources {
	require.NoError(t, testutils.CreateGatekeeperNamespace(cfg))

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	tr := testResources{}
	var dataClient cm.CFDataClient
	if useFakeClient {
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

	if wrapReconciler {
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

	tr := setupTest(ctx, t, true, false)
	mgr := *tr.mgr
	requests := tr.requests
	wm := tr.wm
	c := tr.c

	testutils.StartManager(ctx, t, mgr)

	instanceSyncSet := &syncsetv1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "syncset",
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: []syncsetv1alpha1.GVKEntry{syncsetv1alpha1.GVKEntry(podGVK)},
		},
	}
	require.NoError(t, c.Create(ctx, instanceSyncSet))

	require.Eventually(t, func() bool {
		_, ok := requests.Load(reconcile.Request{NamespacedName: types.NamespacedName{Name: "syncset"}})

		return ok
	}, timeout, tick, "waiting on syncset request to be received")

	require.Eventually(t, func() bool {
		return len(wm.GetManagedGVK()) == 1
	}, timeout, tick, "check watched gvks are populated")

	gvks := wm.GetManagedGVK()
	wantGVKs := []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Pod"},
	}
	require.ElementsMatch(t, wantGVKs, gvks)

	// now delete the sync source and expect no longer watched gvks
	require.NoError(t, c.Delete(ctx, instanceSyncSet))
	require.Eventually(t, func() bool {
		return len(wm.GetManagedGVK()) == 0
	}, timeout, tick, "check watched gvks are deleted")
	require.ElementsMatch(t, []schema.GroupVersionKind{}, wm.GetManagedGVK())
}
