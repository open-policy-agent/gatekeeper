package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const groupDiscoveryPath = "/apis"

func TestAuditTriggerFeatureGate(t *testing.T) {
	oldInterval, oldTrigger := *auditInterval, *enableAuditTrigger
	restore := setAuditGlobalsForReviewTest(t)
	t.Cleanup(func() {
		restore()
		*auditInterval, *enableAuditTrigger = oldInterval, oldTrigger
	})
	testScheme := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(testScheme))
	require.NoError(t, apis.AddToScheme(testScheme))
	require.NoError(t, apiextensionsv1.AddToScheme(testScheme))
	crdDir := filepath.Join("..", "..", "config", "crd", "bases")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(crdDir, "constrainttemplate-customresourcedefinition.yaml")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	apiClient, err := client.New(cfg, client.Options{Scheme: testScheme})
	require.NoError(t, err)
	for _, tt := range []struct {
		name        string
		enabled     bool
		interval    int64
		auditSource string
		missingCRD  bool
	}{
		{name: "disabled with zero interval"},
		{name: "disabled periodic audit without trigger CRD", interval: 1},
		{name: "enabled on-demand only", enabled: true, auditSource: "on-demand"},
		{name: "cold on-demand audit fails a missing required CRD", enabled: true, auditSource: "on-demand", missingCRD: true},
		{name: "enabled periodic audit reports failure", enabled: true, interval: 1, auditSource: "scheduled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			*auditInterval, *enableAuditTrigger = tt.interval, tt.enabled
			core, logs := observer.New(zap.InfoLevel)
			baseLog := zapr.NewLogger(zap.New(core))
			oldLog := log
			log = baseLog.WithName("controller").WithValues(logging.Process, "audit")
			t.Cleanup(func() { log = oldLog })
			ctx := logf.IntoContext(context.Background(), baseLog)
			trigger := &auditv1alpha1.AuditTrigger{
				ObjectMeta: metav1.ObjectMeta{Name: auditv1alpha1.AuditTriggerName},
				Spec:       auditv1alpha1.AuditTriggerSpec{AuditNonce: "gate-test"},
			}
			if tt.enabled {
				_, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
					Paths: []string{filepath.Join(crdDir, "audit.gatekeeper.sh_audittriggers.yaml")},
				})
				require.NoError(t, err)
				require.NoError(t, apiClient.Create(ctx, trigger))
				t.Cleanup(func() { require.NoError(t, apiClient.Delete(ctx, trigger)) })
				if tt.interval > 0 {
					trigger.Status.SuccessfulGeneration = trigger.Generation
					require.NoError(t, apiClient.Status().Update(ctx, trigger))
				}
			}
			var scans, triggerCalls atomic.Int32
			var fault atomic.Pointer[auditHTTPFault]
			if tt.missingCRD || (tt.enabled && tt.interval > 0) {
				code := http.StatusForbidden
				if tt.missingCRD {
					code = http.StatusNotFound
				}
				fault.Store(&auditHTTPFault{
					match: func(req *http.Request) bool {
						return strings.HasSuffix(req.URL.Path, "/customresourcedefinitions/"+crdName)
					},
					code: code,
				})
			}
			managerConfig := rest.CopyConfig(cfg)
			managerConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
				faultTransport := &auditFaultTransport{RoundTripper: rt, fault: &fault}
				return auditRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
					if strings.HasPrefix(req.URL.Path, "/apis/audit.gatekeeper.sh/") {
						triggerCalls.Add(1)
					}
					if strings.HasSuffix(req.URL.Path, "/customresourcedefinitions/"+crdName) && req.Method == http.MethodGet {
						scans.Add(1)
					}
					return faultTransport.RoundTrip(req)
				})
			})
			skipNameValidation := true
			realManager, err := manager.New(managerConfig, manager.Options{
				Scheme:  testScheme,
				Metrics: metricsserver.Options{BindAddress: "0"},
				Controller: controllerconfig.Controller{
					SkipNameValidation: &skipNameValidation,
				},
			})
			require.NoError(t, err)
			mgr := &auditRecordingManager{Manager: realManager}
			deps := &Dependencies{}
			if tt.enabled {
				deps.InitialDataReady = func(bool) bool { return false }
			}
			require.NoError(t, AddToManager(mgr, deps))
			if !tt.enabled && tt.interval == 0 {
				require.Nil(t, mgr.audit)
				require.Zero(t, mgr.added)
				return
			}
			wantAdded := 1
			if tt.enabled {
				wantAdded++
			}
			require.Equal(t, wantAdded, mgr.added)
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- mgr.Start(runCtx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(10 * time.Second):
					t.Error("manager did not stop")
				}
				select {
				case <-mgr.audit.stopper:
				case <-time.After(10 * time.Second):
					t.Error("audit loop did not stop")
				}
			})
			require.Eventually(t, func() bool {
				if !tt.enabled {
					return scans.Load() > 0
				}
				if err := apiClient.Get(ctx, auditTriggerKey, trigger); err != nil || trigger.Status.LastRun == nil {
					return false
				}
				if tt.interval > 0 || tt.missingCRD {
					return trigger.Status.FailedGeneration == trigger.Generation && trigger.Status.LastRun.Phase == "Failed"
				}
				return trigger.Status.SuccessfulGeneration >= trigger.Generation
			}, 15*time.Second, 20*time.Millisecond)
			if !tt.enabled {
				require.Zero(t, triggerCalls.Load(), "disabled audits must not access the trigger API")
			}
			require.Eventually(t, func() bool {
				return logs.FilterField(zap.String(logging.EventType, "audit_finished")).Len() > 0
			}, 15*time.Second, 20*time.Millisecond)
			for _, eventType := range []string{"audit_started", "audit_finished"} {
				entries := logs.FilterField(zap.String(logging.EventType, eventType)).All()
				require.NotEmpty(t, entries)
				for _, entry := range entries {
					fields := entry.ContextMap()
					require.Equal(t, "controller", entry.LoggerName)
					require.Equal(t, "audit", fields[logging.Process])
					require.NotEmpty(t, fields[logging.AuditID])
					if tt.enabled {
						require.Equal(t, tt.auditSource, fields["auditSource"])
					}
				}
			}
		})
	}
}

type auditRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f auditRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type auditScanFixture struct {
	mgr            manager.Manager
	deps           *Dependencies
	client         client.Client
	constraint     *unstructured.Unstructured
	constraintPath string
	faults         atomic.Pointer[auditHTTPFault]
}

func newAuditScanFixture(t *testing.T) *auditScanFixture {
	t.Helper()
	f := &auditScanFixture{}
	testScheme := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(testScheme))
	require.NoError(t, apis.AddToScheme(testScheme))
	require.NoError(t, apiextensionsv1.AddToScheme(testScheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	ctx := context.Background()
	apiClient, err := client.New(cfg, client.Options{Scheme: testScheme})
	require.NoError(t, err)
	kinds, err := (&Manager{mgr: &auditResourcesTestManager{config: cfg}}).getAllConstraintKinds()
	require.NoError(t, err, "an absent constraint API is an empty audit, not a discovery failure")
	require.Empty(t, kinds)

	opa := testutils.SetupDataClient(t)
	template := fakes.DenyAllRegoTemplate()
	template.Name = "auditdenyall"
	template.Spec.CRD.Spec.Names.Kind = "AuditDenyAll"
	crd, err := opa.CreateCRD(ctx, template)
	require.NoError(t, err)
	versionedCRD := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, apiextensionsv1.Convert_apiextensions_CustomResourceDefinition_To_v1_CustomResourceDefinition(crd, versionedCRD, nil))
	require.NoError(t, apiClient.Create(ctx, versionedCRD))
	_, err = opa.AddTemplate(ctx, template)
	require.NoError(t, err)

	constraint := fakes.ConstraintFor("AuditDenyAll")
	constraint.Object["spec"] = map[string]interface{}{
		"enforcementAction": "dryrun",
		"match": map[string]interface{}{
			"kinds": []interface{}{
				map[string]interface{}{"apiGroups": []interface{}{""}, "kinds": []interface{}{"ConfigMap"}},
			},
			"namespaces": []interface{}{"default"},
		},
	}
	require.Eventually(t, func() bool {
		err := apiClient.Create(ctx, constraint)
		return err == nil
	}, 10*time.Second, 50*time.Millisecond, "constraint CRD did not become available")
	_, err = opa.AddConstraint(ctx, constraint)
	require.NoError(t, err)
	for _, name := range []string{"audit-a", "audit-b"} {
		require.NoError(t, apiClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		}))
	}

	managerConfig := rest.CopyConfig(cfg)
	managerConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &auditFaultTransport{RoundTripper: rt, fault: &f.faults}
	})
	skipNameValidation := true
	f.mgr, err = manager.New(managerConfig, manager.Options{
		Scheme:  testScheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Controller: controllerconfig.Controller{
			SkipNameValidation: &skipNameValidation,
		},
	})
	require.NoError(t, err)
	f.deps = &Dependencies{
		Client:           opa,
		InitialDataReady: func(bool) bool { return true },
		ProcessExcluder:  process.New(),
		ExpansionSystem:  expansion.NewSystem(nil),
	}
	f.client, f.constraint = apiClient, constraint
	f.constraintPath = "/apis/" + constraintsGV + "/" + versionedCRD.Spec.Names.Plural
	return f
}

// Do not fault the initial REST mapping or direct constraint discovery.
func (f *auditScanFixture) afterConstraintProbe(match func(*http.Request) bool) func(*http.Request) bool {
	var scanning atomic.Bool
	return func(req *http.Request) bool {
		if req.URL.Path == f.constraintPath && req.URL.Query().Get("limit") == "1" {
			scanning.Store(true)
		}
		return scanning.Load() && match(req)
	}
}

func TestAuditResourcesRejectsEmptyDiscovery(t *testing.T) {
	t.Cleanup(setAuditGlobalsForReviewTest(t))
	*apiCacheDir = t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json;g=apidiscovery.k8s.io;v=v2;as=APIGroupDiscoveryList")
		_, err := io.WriteString(w, `{"apiVersion":"apidiscovery.k8s.io/v2","kind":"APIGroupDiscoveryList","items":[]}`)
		if err != nil {
			t.Errorf("writing discovery: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	am := &Manager{mgr: &auditResourcesTestManager{config: &rest.Config{Host: server.URL}}}
	require.ErrorContains(t, am.auditResources(t.Context(), nil, nil, nil, nil, "", nil), "no listable kinds")
}

// Exercise the scan callbacks used by the periodic loop and the trigger runner
// synchronously so a second tick can't overwrite the evidence from a failed scan.
func TestAuditResourceCoverage(t *testing.T) {
	restore := setAuditGlobalsForReviewTest(t)
	oldMatch, oldChunk, oldCache, oldTrigger, oldLog := *auditMatchKindOnly, *auditChunkSize, *auditFromCache, *enableAuditTrigger, log
	t.Cleanup(func() {
		restore()
		*auditMatchKindOnly, *auditChunkSize, *auditFromCache, *enableAuditTrigger, log = oldMatch, oldChunk, oldCache, oldTrigger, oldLog
	})
	*apiCacheDir, *auditFromCache = t.TempDir(), false
	f := newAuditScanFixture(t)
	const restrictedNS = "audit-restricted"
	const nsPath = "/api/v1/namespaces/"
	require.NoError(t, f.client.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: restrictedNS}}))
	core, logs := observer.New(zap.InfoLevel)
	log = zapr.NewLogger(zap.New(core)).WithName("controller").WithValues(logging.Process, "audit")
	ctx := logf.IntoContext(context.Background(), log)

	for _, mode := range []string{"periodic", "trigger scheduled", "trigger on-demand"} {
		t.Run(mode, func(t *testing.T) {
			*enableAuditTrigger = mode != "periodic"
			am, err := New(f.mgr, f.deps)
			require.NoError(t, err)
			trigger := &auditv1alpha1.AuditTrigger{
				ObjectMeta: metav1.ObjectMeta{Name: auditv1alpha1.AuditTriggerName},
			}
			if *enableAuditTrigger {
				trigger.Spec.AuditNonce = "initial"
				require.NoError(t, f.client.Create(ctx, trigger))
				t.Cleanup(func() { require.NoError(t, f.client.Delete(ctx, trigger)) })
			}
			runner := auditTriggerRunner{reader: f.client, client: f.client, audit: am.audit, now: time.Now}
			scan := func(t *testing.T, nonce string) error {
				t.Helper()
				if !*enableAuditTrigger {
					return am.audit(ctx)
				}
				require.NoError(t, f.client.Get(ctx, auditTriggerKey, trigger))
				trigger.Spec.AuditNonce = nonce
				require.NoError(t, f.client.Update(ctx, trigger))
				err := runner.run(ctx, mode == "trigger on-demand")
				require.NoError(t, f.client.Get(ctx, auditTriggerKey, trigger))
				return err
			}
			readStatus := func(t *testing.T) map[string]interface{} {
				t.Helper()
				u := f.constraint.DeepCopy()
				require.NoError(t, f.client.Get(ctx, client.ObjectKeyFromObject(u), u))
				s, found, err := unstructured.NestedMap(u.Object, "status")
				require.NoError(t, err)
				require.True(t, found)
				return s
			}
			checkTotals := func(t *testing.T, status map[string]interface{}, want int64) {
				t.Helper()
				require.Equal(t, want, status["totalViolations"])
				am.reporter.mu.RLock()
				defer am.reporter.mu.RUnlock()
				require.Equal(t, want, am.reporter.totalViolationsPerEnforcementAction[util.Dryrun])
			}

			for _, tc := range []struct {
				name            string
				path            string
				code            int
				legacyDiscovery bool
				matchOnly       bool
				nextPage        bool
				bestEffort      bool
				reviewFailure   bool
			}{
				{name: "one forbidden LIST", path: "/api/v1/secrets", code: http.StatusForbidden, bestEffort: true},
				{name: "one forbidden namespace", path: nsPath + restrictedNS, code: http.StatusForbidden, matchOnly: true, bestEffort: true},
				{name: "forbidden namespace and policy error", path: nsPath + restrictedNS, code: http.StatusForbidden, matchOnly: true, reviewFailure: true},
				{name: "no readable namespace", path: nsPath, code: http.StatusForbidden, matchOnly: true},
				{name: "missing namespace", path: nsPath + "default", code: http.StatusNotFound, matchOnly: true},
				{name: "one unavailable group", path: "/apis/batch/v1", code: http.StatusServiceUnavailable, legacyDiscovery: true, matchOnly: true, bestEffort: true},
				{name: "partial discovery with policy error", path: "/apis/batch/v1", code: http.StatusServiceUnavailable, legacyDiscovery: true, matchOnly: true, reviewFailure: true},
				{name: "no usable discovery", path: "/api/v1", code: http.StatusServiceUnavailable, legacyDiscovery: true, matchOnly: true},
				{name: "total discovery outage", path: groupDiscoveryPath, code: http.StatusServiceUnavailable, matchOnly: true},
				{name: "no readable kind", path: "/api/v1/configmaps", code: http.StatusForbidden, matchOnly: true},
				{name: "forbidden continuation", path: "/api/v1/configmaps", code: http.StatusForbidden, matchOnly: true, nextPage: true},
				{name: "forbidden mapping", path: "/apis/batch/v1", code: http.StatusForbidden, legacyDiscovery: true},
				{name: "resource LIST not found", path: "/api/v1/secrets", code: http.StatusNotFound},
			} {
				t.Run(tc.name, func(t *testing.T) {
					*auditMatchKindOnly, *auditChunkSize = true, 1
					require.NoError(t, scan(t, tc.name+" seed"))
					previous := readStatus(t)
					checkTotals(t, previous, 2)
					previousGeneration := trigger.Status.SuccessfulGeneration

					// Only the new run can have seen this third violating ConfigMap.
					cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "audit-new", Namespace: "default"}}
					require.NoError(t, f.client.Create(ctx, cm))
					t.Cleanup(func() { require.NoError(t, f.client.Delete(ctx, cm)) })
					if tc.path == nsPath+restrictedNS {
						other := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cm.Name, Namespace: restrictedNS}}
						require.NoError(t, f.client.Create(ctx, other))
						t.Cleanup(func() { require.NoError(t, f.client.Delete(ctx, other)) })
					}
					fault := &auditHTTPFault{
						code: tc.code, legacyDiscovery: tc.legacyDiscovery,
						match: func(req *http.Request) bool {
							return req.URL.Path == tc.path && (!tc.nextPage || req.URL.Query().Get("continue") != "")
						},
					}
					if tc.path == groupDiscoveryPath {
						fault.match = f.afterConstraintProbe(fault.match)
					}
					if tc.path == nsPath {
						fault.match = func(req *http.Request) bool {
							name, ok := strings.CutPrefix(req.URL.Path, nsPath)
							return req.Method == http.MethodGet && ok && name != "" && !strings.Contains(name, "/")
						}
					}
					if tc.name == "forbidden mapping" {
						// Discover the batch kinds successfully; deny their subsequent REST mapping, not a LIST.
						var requests atomic.Int32
						fault.match = func(req *http.Request) bool {
							return req.URL.Path == tc.path && requests.Add(1) > 1
						}
					}
					*auditMatchKindOnly = tc.matchOnly
					if !tc.nextPage {
						*auditChunkSize = defaultListLimit
					}
					logs.TakeAll()
					if tc.reviewFailure {
						am.opa = auditClientWithoutAuditEnforcementPoint(t)
					}
					f.faults.Store(fault)
					err := scan(t, tc.name)
					f.faults.Store(nil)
					am.opa = f.deps.Client
					require.Positive(t, fault.hits.Load(), "fault did not reach the scan")
					if tc.reviewFailure {
						require.ErrorContains(t, err, "reviewing object")
					}

					status := readStatus(t)
					if tc.bestEffort {
						// Both the skipped scope and the readable violation must be visible to operators.
						errors := logs.FilterLevelExact(zap.ErrorLevel)
						switch {
						case tc.legacyDiscovery:
							require.NotEmpty(t, errors.Filter(func(e observer.LoggedEntry) bool {
								message, _ := e.ContextMap()["error"].(string)
								return strings.Contains(message, "batch/v1")
							}).All())
						case tc.path == nsPath+restrictedNS:
							require.NotEmpty(t, errors.FilterField(zap.String("objNs", restrictedNS)).All())
						default:
							require.NotEmpty(t, errors.FilterField(zap.String("kind", "Secret")).All())
						}
						require.NotEmpty(t, logs.FilterField(zap.String(logging.EventType, "violation_audited")).
							FilterField(zap.String(logging.ResourceName, cm.Name)).All())
					}
					if tc.bestEffort && !*enableAuditTrigger {
						require.NoError(t, err, "constraint total before/after: %v/%v; timestamp changed: %t; constraint summaries: %d",
							previous["totalViolations"], status["totalViolations"], previous["auditTimestamp"] != status["auditTimestamp"],
							logs.FilterField(zap.String(logging.EventType, "constraint_audited")).Len())
					} else {
						require.Error(t, err)
						require.Equal(t, previous, status, "incomplete scan replaced prior status")
						checkTotals(t, status, 2)
						require.Empty(t, logs.FilterField(zap.String(logging.EventType, "constraint_audited")).All())
						if *enableAuditTrigger {
							require.Equal(t, trigger.Generation, trigger.Status.FailedGeneration)
							require.Equal(t, previousGeneration, trigger.Status.SuccessfulGeneration)
							require.Equal(t, "Failed", trigger.Status.LastRun.Phase)
						}

						// Fault is cleared, but only a fresh scan may replace the old results.
						require.NoError(t, scan(t, tc.name+" recovered"))
						status = readStatus(t)
						if *enableAuditTrigger {
							require.Equal(t, trigger.Generation, trigger.Status.SuccessfulGeneration)
							require.Equal(t, "Succeeded", trigger.Status.LastRun.Phase)
						}
					}
					checkTotals(t, status, 3)
					timestamp, ok := status["auditTimestamp"].(string)
					require.True(t, ok)
					require.NotEqual(t, previous["auditTimestamp"], timestamp)
					require.Contains(t, status["violations"], map[string]interface{}{
						"group": "", "version": "v1", "kind": "ConfigMap", "namespace": cm.Namespace, "name": cm.Name,
						"message": "denyall", "enforcementAction": "dryrun",
					})
					require.Len(t, logs.FilterField(zap.String(logging.AuditID, timestamp)).
						FilterField(zap.String(logging.EventType, "constraint_audited")).
						FilterField(zap.String(logging.ConstraintName, f.constraint.GetName())).
						FilterField(zap.String(logging.ConstraintViolations, "3")).All(), 1)
				})
			}
		})
	}
}

func TestDeferredAuditPreservesDiskExport(t *testing.T) {
	oldGate, oldMatch, oldCache := *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache
	t.Cleanup(func() { *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache = oldGate, oldMatch, oldCache })
	t.Cleanup(setAuditGlobalsForReviewTest(t))
	*apiCacheDir, *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache = t.TempDir(), true, true, false
	f := newAuditScanFixture(t)
	*exportutil.ExportEnabled = true
	ctx := t.Context()
	dir := t.TempDir()
	sys := export.NewSystem()
	require.NoError(t, sys.UpsertConnection(ctx, map[string]interface{}{"path": dir, "maxAuditResults": float64(1)}, *exportutil.AuditConnection, disk.Name))
	t.Cleanup(func() { require.NoError(t, sys.CloseConnection(*exportutil.AuditConnection)) })
	f.deps.ExportSystem = sys
	retained := func() (string, string) {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(dir, *exportutil.AuditChannel, "*.log"))
		require.NoError(t, err)
		require.Len(t, files, 1)
		data, err := os.ReadFile(files[0])
		require.NoError(t, err)
		return files[0], string(data)
	}
	warm, err := New(f.mgr, f.deps)
	require.NoError(t, err)
	require.NoError(t, warm.audit(ctx))
	original, before := retained()
	require.Contains(t, before, "audit-a")
	require.Contains(t, before, "audit-b")

	ready := false
	f.deps.InitialDataReady = func(bool) bool { return ready }
	cold, err := New(f.mgr, f.deps)
	require.NoError(t, err)
	for range 3 {
		require.ErrorIs(t, cold.audit(ctx), errAwaitingInitialData)
		name, data := retained()
		require.Equal(t, original, name, "a readiness check must not evict a real export")
		require.Equal(t, before, data)
	}
	require.Zero(t, cold.reporter.auditExportPublished.Load())
	require.True(t, cold.reporter.startTime.IsZero())
	require.True(t, cold.reporter.endTime.IsZero())

	ready = true
	require.NoError(t, cold.audit(ctx))
	name, data := retained()
	require.NotEqual(t, original, name)
	require.Contains(t, data, "audit-a")
	require.Contains(t, data, "audit-b")
	require.EqualValues(t, 4, cold.reporter.auditExportPublished.Load(), "one start, two violations, one end")

	// No constraints is a real empty audit, even before initial readiness.
	require.NoError(t, f.client.Delete(ctx, f.constraint))
	ready = false
	empty, err := New(f.mgr, f.deps)
	require.NoError(t, err)
	require.NoError(t, empty.audit(ctx))
	emptyName, emptyData := retained()
	require.NotEqual(t, name, emptyName)
	require.Contains(t, emptyData, exportutil.AuditStartedMsg)
	require.Contains(t, emptyData, exportutil.AuditCompletedMsg)
	require.EqualValues(t, 2, empty.reporter.auditExportPublished.Load())
}

// Exercise the real controller and audit loop, not a runner with a stub audit callback.
func TestAuditTriggerProductionFailures(t *testing.T) {
	oldInterval, oldMatchKindOnly, oldChunkSize := *auditInterval, *auditMatchKindOnly, *auditChunkSize
	oldFromCache, oldExport, oldTrigger := *auditFromCache, *exportutil.ExportEnabled, *enableAuditTrigger
	restore := setAuditGlobalsForReviewTest(t)
	t.Cleanup(func() {
		restore()
		*auditInterval, *auditMatchKindOnly, *auditChunkSize = oldInterval, oldMatchKindOnly, oldChunkSize
		*auditFromCache, *exportutil.ExportEnabled, *enableAuditTrigger = oldFromCache, oldExport, oldTrigger
	})
	*auditInterval, *auditMatchKindOnly, *auditChunkSize = 0, true, 1
	*auditFromCache = false
	*enableAuditTrigger = true
	*apiCacheDir = t.TempDir()

	f := newAuditScanFixture(t)
	apiClient, constraint := f.client, f.constraint
	mgr := &auditRecordingManager{Manager: f.mgr}
	require.NoError(t, AddToManager(mgr, f.deps))
	ctx := context.Background()
	runCtx, cancel := context.WithCancel(ctx)
	managerDone := make(chan error, 1)
	go func() { managerDone <- mgr.Start(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-managerDone:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("manager did not stop")
		}
		select {
		case <-mgr.audit.stopper:
		case <-time.After(10 * time.Second):
			t.Error("audit loop did not stop")
		}
	})
	require.True(t, mgr.GetCache().WaitForCacheSync(runCtx))

	trigger := &auditv1alpha1.AuditTrigger{
		ObjectMeta: metav1.ObjectMeta{Name: auditv1alpha1.AuditTriggerName},
		Spec:       auditv1alpha1.AuditTriggerSpec{AuditNonce: "initial"},
	}
	// Arm the first fault only after the manager and its informer have started.
	cases := []struct {
		name  string
		code  int
		match func(*http.Request) bool
	}{
		{
			name: "constraint discovery unavailable",
			code: http.StatusServiceUnavailable,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/apis/"+constraintsGV
			},
		},
		{
			name: "CRD check forbidden",
			code: http.StatusForbidden,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"+crdName
			},
		},
		{
			name: "required CRD not found",
			code: http.StatusNotFound,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"+crdName
			},
		},
		{
			name: "partial resource discovery",
			code: http.StatusServiceUnavailable,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/apis/"+constraintsGV
			},
		},
		{
			name: "constraint match listing unavailable",
			code: http.StatusServiceUnavailable,
			match: func(r *http.Request) bool {
				return r.URL.Path == f.constraintPath && r.URL.Query().Get("limit") == ""
			},
		},
		{
			name: "resource list unavailable",
			code: http.StatusServiceUnavailable,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/api/v1/configmaps"
			},
		},
		{
			name: "resource pagination unavailable",
			code: http.StatusServiceUnavailable,
			match: func(r *http.Request) bool {
				return r.URL.Path == "/api/v1/configmaps" && r.URL.Query().Get("continue") != ""
			},
		},
	}
	var successfulGeneration int64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := constraint.DeepCopy()
			require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(constraint), previous))
			previousStatus := previous.DeepCopy().Object["status"]
			previousTimestamp, _, err := unstructured.NestedString(previous.Object, "status", "auditTimestamp")
			require.NoError(t, err)
			mgr.audit.reporter.mu.RLock()
			previousTotals := make(map[util.EnforcementAction]int64)
			for action, total := range mgr.audit.reporter.totalViolationsPerEnforcementAction {
				previousTotals[action] = total
			}
			mgr.audit.reporter.mu.RUnlock()
			fault := &auditHTTPFault{match: tc.match, code: tc.code, count: 1}
			if tc.name == "partial resource discovery" {
				fault.legacyDiscovery, fault.count = true, 2
				fault.match = f.afterConstraintProbe(fault.match)
			}
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("last trigger status: %+v; injected faults: %d", trigger.Status, fault.hits.Load())
				}
			})
			f.faults.Store(fault)
			if err := apiClient.Get(ctx, auditTriggerKey, trigger); apierrors.IsNotFound(err) {
				require.NoError(t, apiClient.Create(ctx, trigger))
			} else {
				require.NoError(t, err)
				trigger.Spec.AuditNonce = tc.name
				require.NoError(t, apiClient.Update(ctx, trigger))
			}
			generation := trigger.Generation
			require.Eventually(t, func() bool {
				if err := apiClient.Get(ctx, auditTriggerKey, trigger); err != nil {
					return false
				}
				return trigger.Status.FailedGeneration == generation
			}, 20*time.Second, 25*time.Millisecond, "audit did not record failure: %+v", trigger.Status)
			require.Equal(t, fault.count, fault.hits.Load(), "HTTP fault was not exercised")
			require.Equal(t, successfulGeneration, trigger.Status.SuccessfulGeneration)
			require.Equal(t, "Failed", trigger.Status.LastRun.Phase)
			require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(constraint), previous))
			require.Equal(t, previousStatus, previous.Object["status"], "failed audit replaced previous results")
			mgr.audit.reporter.mu.RLock()
			endTime := mgr.audit.reporter.endTime
			currentTotals := make(map[util.EnforcementAction]int64)
			for action, total := range mgr.audit.reporter.totalViolationsPerEnforcementAction {
				currentTotals[action] = total
			}
			mgr.audit.reporter.mu.RUnlock()
			require.False(t, endTime.Before(trigger.Status.LastRun.StartedAt.Time))
			require.Equal(t, previousTotals, currentTotals, "failed audit replaced previous violation metrics")

			// A fresh nonce must recover through the same production path.
			trigger.Spec.AuditNonce = tc.name + "-recovered"
			require.NoError(t, apiClient.Update(ctx, trigger))
			successfulGeneration = trigger.Generation
			require.Eventually(t, func() bool {
				if err := apiClient.Get(ctx, auditTriggerKey, trigger); err != nil {
					return false
				}
				return trigger.Status.SuccessfulGeneration == successfulGeneration
			}, 20*time.Second, 25*time.Millisecond, "audit did not recover: %+v", trigger.Status)
			require.Equal(t, "Succeeded", trigger.Status.LastRun.Phase)
			require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(constraint), previous))
			total, _, err := unstructured.NestedInt64(previous.Object, "status", "totalViolations")
			require.NoError(t, err)
			require.EqualValues(t, 2, total)
			timestamp, _, err := unstructured.NestedString(previous.Object, "status", "auditTimestamp")
			require.NoError(t, err)
			require.NotEmpty(t, timestamp)
			require.NotEqual(t, previousTimestamp, timestamp, "recovery did not publish fresh audit results")
		})
		if t.Failed() {
			break
		}
	}
}

type auditRecordingManager struct {
	manager.Manager
	audit *Manager
	added int
}

func (m *auditRecordingManager) Add(r manager.Runnable) error {
	m.added++
	if am, ok := r.(*Manager); ok {
		m.audit = am
	}
	return m.Manager.Add(r)
}

type auditHTTPFault struct {
	match           func(*http.Request) bool
	code            int
	legacyDiscovery bool
	count           int32
	hits            atomic.Int32
}

type auditFaultTransport struct {
	http.RoundTripper
	fault *atomic.Pointer[auditHTTPFault]
}

func (rt *auditFaultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fault := rt.fault.Load()
	if fault != nil && fault.legacyDiscovery && (req.URL.Path == "/api" || req.URL.Path == groupDiscoveryPath) {
		// Exercise the supported legacy partial-discovery path on servers with aggregated discovery.
		req = req.Clone(req.Context())
		req.Header.Set("Accept", "application/json")
	}
	if fault == nil || req.Method != http.MethodGet || !fault.match(req) {
		return rt.RoundTripper.RoundTrip(req)
	}
	if fault.hits.Add(1) == fault.count {
		rt.fault.CompareAndSwap(fault, nil)
	}
	status := apierrors.NewServiceUnavailable("injected audit failure").ErrStatus
	status.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}
	switch fault.code {
	case http.StatusForbidden:
		status.Reason = metav1.StatusReasonForbidden
		status.Code = http.StatusForbidden
	case http.StatusNotFound:
		status.Reason = metav1.StatusReasonNotFound
		status.Code = http.StatusNotFound
	}
	body, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: fault.code,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}, nil
}
