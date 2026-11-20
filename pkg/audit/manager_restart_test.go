package audit

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constrainttemplate"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestAuditTriggerColdStart(t *testing.T) {
	oldInterval, oldTrigger, oldMatchKinds, oldFromCache := *auditInterval, *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache
	t.Cleanup(func() {
		*auditInterval, *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache = oldInterval, oldTrigger, oldMatchKinds, oldFromCache
	})
	t.Cleanup(setAuditGlobalsForReviewTest(t))
	*auditInterval, *enableAuditTrigger, *auditMatchKindOnly, *auditFromCache = 0, true, true, false
	*apiCacheDir = t.TempDir()
	restore, err := operations.SetForTest(operations.Audit)
	require.NoError(t, err)
	t.Cleanup(restore)
	t.Setenv("POD_NAMESPACE", corev1.NamespaceDefault)
	ctx := t.Context()
	const timeout, tick = 15 * time.Second, 25 * time.Millisecond

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	require.NoError(t, apis.AddToScheme(s))
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	api, err := client.New(cfg, client.Options{Scheme: s})
	require.NoError(t, err)

	opa := testutils.SetupDataClient(t)
	template := fakes.DenyAllRegoTemplate()
	template.Name, template.Spec.CRD.Spec.Names.Kind = "auditdenyall", "AuditDenyAll"
	versioned := &templatesv1beta1.ConstraintTemplate{}
	require.NoError(t, s.Convert(template, versioned, nil))
	require.NoError(t, api.Create(ctx, versioned))
	crd, err := opa.CreateCRD(ctx, template)
	require.NoError(t, err)
	versionedCRD := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, apiextensionsv1.Convert_apiextensions_CustomResourceDefinition_To_v1_CustomResourceDefinition(crd, versionedCRD, nil))
	require.NoError(t, api.Create(ctx, versionedCRD))
	constraint := fakes.ConstraintFor(template.Spec.CRD.Spec.Names.Kind)
	const match, kinds, configMapKind = "match", "kinds", "ConfigMap"
	constraint.Object["spec"] = map[string]interface{}{
		"enforcementAction": "dryrun",
		match: map[string]interface{}{
			kinds:        []interface{}{map[string]interface{}{"apiGroups": []interface{}{""}, kinds: []interface{}{configMapKind}}},
			"namespaces": []interface{}{corev1.NamespaceDefault},
			"labelSelector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"audit-test": "matched"},
			},
		},
	}
	require.Eventually(t, func() bool { return api.Create(ctx, constraint) == nil }, timeout, tick)
	configMap := func(t *testing.T, name, label string) {
		t.Helper()
		require.NoError(t, api.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault, Name: name, Labels: map[string]string{"audit-test": label},
		}}))
	}
	configMap(t, "first", "matched")
	configMap(t, "allowed", "other")
	trigger := &auditv1alpha1.AuditTrigger{
		ObjectMeta: metav1.ObjectMeta{Name: auditv1alpha1.AuditTriggerName},
		Spec:       auditv1alpha1.AuditTriggerSpec{AuditNonce: "warm"},
	}
	type auditResult struct {
		AuditTimestamp  string            `json:"auditTimestamp"`
		TotalViolations int64             `json:"totalViolations"`
		Violations      []StatusViolation `json:"violations"`
	}
	status := func(t *testing.T) auditResult {
		t.Helper()
		c := constraint.DeepCopy()
		require.NoError(t, api.Get(ctx, client.ObjectKeyFromObject(c), c))
		st, found, err := unstructured.NestedMap(c.Object, "status")
		require.NoError(t, err)
		require.True(t, found)
		var result auditResult
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(st, &result))
		return result
	}
	var previous auditResult
	if !t.Run("complete result before restart", func(t *testing.T) {
		tracker := startRestartTestManager(t, cfg, s, opa, "warm", nil)
		require.Eventually(t, tracker.Satisfied, timeout, tick)
		require.NoError(t, api.Create(ctx, trigger))
		require.Eventually(t, func() bool {
			return api.Get(ctx, auditTriggerKey, trigger) == nil && trigger.Status.SuccessfulGeneration == trigger.Generation
		}, timeout, tick)
		previous = status(t)
		require.EqualValues(t, 1, previous.TotalViolations)
		require.Len(t, previous.Violations, 1)
		require.Equal(t, "first", previous.Violations[0].Name)
		require.NotEmpty(t, previous.AuditTimestamp)
	}) {
		return
	}

	for _, tc := range []struct {
		name       string
		configMaps []string
		unreadable bool
	}{
		{name: "pending request without periodic ticks", configMaps: []string{"first", "second"}},
		{name: "scheduled fallback", configMaps: []string{"first", "second", "third"}, unreadable: true},
	} {
		if !t.Run(tc.name, func(t *testing.T) {
			// No audit process is running. This write is the only request for the new data.
			newName := tc.configMaps[len(tc.configMaps)-1]
			configMap(t, newName, "matched")
			trigger.Spec.AuditNonce = newName
			require.NoError(t, api.Update(ctx, trigger))
			uid, gen := trigger.UID, trigger.Generation
			require.Less(t, trigger.Status.SuccessfulGeneration, gen)

			// Block only the new pod's ConstraintPodStatus creation, before the
			// real reconciler calls AddConstraint. Discovery and LISTs still work.
			entered, release := make(chan struct{}), make(chan struct{})
			signalHeld := sync.OnceFunc(func() { close(entered) })
			var fault atomic.Pointer[auditHTTPFault]
			if tc.unreadable {
				*auditInterval = 1
				fault.Store(&auditHTTPFault{code: http.StatusForbidden, match: func(r *http.Request) bool {
					return strings.HasSuffix(r.URL.Path, "/audittriggers/"+auditv1alpha1.AuditTriggerName)
				}})
			}
			coldConfig := rest.CopyConfig(cfg)
			coldConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
				rt = &auditFaultTransport{RoundTripper: rt, fault: &fault}
				return auditRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/constraintpodstatuses") {
						signalHeld()
						select {
						case <-release:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
					}
					return rt.RoundTrip(r)
				})
			})
			coldOPA := testutils.SetupDataClient(t)
			var coldChecks atomic.Int32
			tracker := startRestartTestManager(t, coldConfig, s, coldOPA, newName, &coldChecks)
			select {
			case <-entered:
			case <-time.After(timeout):
				t.Fatal("constraint reconciler did not reach the ingestion barrier")
			}
			_, err := coldOPA.GetConstraint(constraint)
			require.Error(t, err)
			require.Eventually(t, tracker.Populated, timeout, tick)
			require.False(t, tracker.Satisfied())
			require.Eventually(t, func() bool {
				if api.Get(ctx, auditTriggerKey, trigger) != nil {
					return false
				}
				if coldChecks.Load() >= 2 || trigger.Status.SuccessfulGeneration == gen || trigger.Status.FailedGeneration == gen {
					return true
				}
				if tc.unreadable {
					// An unguarded scheduled fallback can't credit the unreadable
					// generation, but the changed constraint timestamp exposes it.
					c := constraint.DeepCopy()
					if api.Get(ctx, client.ObjectKeyFromObject(c), c) == nil {
						ts, _, _ := unstructured.NestedString(c.Object, "status", "auditTimestamp")
						return ts != previous.AuditTimestamp
					}
				}
				return false
			}, timeout, tick)
			cold := status(t)
			t.Logf("ingestion held: uid=%s generation=%d acknowledged=%d successful=%d failed=%d totalViolations=%v",
				trigger.UID, gen, trigger.Status.AcknowledgedGeneration, trigger.Status.SuccessfulGeneration, trigger.Status.FailedGeneration, cold.TotalViolations)
			assert.Less(t, trigger.Status.SuccessfulGeneration, gen, "credited a cold engine's scan")
			assert.Less(t, trigger.Status.FailedGeneration, gen, "consumed the request before ingestion")
			assert.Equal(t, previous, cold, "replaced a complete result before ingestion")

			close(release)
			require.Eventually(t, tracker.Satisfied, timeout, tick)
			if tc.unreadable {
				// The warmed scheduled fallback publishes normally, without
				// claiming to have covered a generation it could not read.
				require.Positive(t, fault.Load().hits.Load())
				require.Eventually(t, func() bool {
					c := constraint.DeepCopy()
					if err := api.Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
						return false
					}
					total, _, _ := unstructured.NestedInt64(c.Object, "status", "totalViolations")
					return total == int64(len(tc.configMaps))
				}, timeout, tick)
				require.NoError(t, api.Get(ctx, auditTriggerKey, trigger))
				require.Less(t, trigger.Status.SuccessfulGeneration, gen)
				fault.Store(nil)
			}
			require.Eventually(t, func() bool {
				return api.Get(ctx, auditTriggerKey, trigger) == nil && trigger.Status.SuccessfulGeneration == gen &&
					trigger.Status.LastRun != nil && trigger.Status.LastRun.Phase == "Succeeded"
			}, timeout, tick)
			require.Equal(t, uid, trigger.UID)
			require.Equal(t, gen, trigger.Generation)
			require.Equal(t, gen, trigger.Status.LastRun.CoveredGeneration)
			published := status(t)
			t.Logf("ingestion released: uid=%s generation=%d successful=%d totalViolations=%v",
				trigger.UID, trigger.Generation, trigger.Status.SuccessfulGeneration, published.TotalViolations)
			require.NotEqual(t, previous.AuditTimestamp, published.AuditTimestamp)
			require.EqualValues(t, len(tc.configMaps), published.TotalViolations)
			var names []string
			for i := range published.Violations {
				names = append(names, published.Violations[i].Name)
			}
			require.ElementsMatch(t, tc.configMaps, names)
			previous = published
		}) {
			return
		}
	}
}

func startRestartTestManager(t *testing.T, cfg *rest.Config, s *runtime.Scheme, opa *constraintclient.Client, podName string, coldChecks *atomic.Int32) *readiness.Tracker {
	t.Helper()
	t.Setenv("POD_NAME", podName)
	skipNameValidation := true
	m, err := manager.New(cfg, manager.Options{
		Scheme:     s,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: controllerconfig.Controller{SkipNameValidation: &skipNameValidation},
	})
	require.NoError(t, err)
	mgr := &auditRecordingManager{Manager: m}
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)
	wm, err := watch.New(mgr.GetCache())
	require.NoError(t, err)
	require.NoError(t, mgr.Add(wm))
	excluder := process.New()
	adder := constrainttemplate.Adder{
		CFClient: opa, WatchManager: wm, Tracker: tracker, ProcessExcluder: excluder,
		GetPod: func(context.Context) (*corev1.Pod, error) {
			return fakes.Pod(fakes.WithNamespace(corev1.NamespaceDefault), fakes.WithName(podName)), nil
		},
	}
	require.NoError(t, adder.Add(mgr))
	require.NoError(t, AddToManager(mgr, &Dependencies{
		Client: opa, ProcessExcluder: excluder, ExpansionSystem: expansion.NewSystem(nil),
		InitialDataReady: func() bool {
			ready := tracker.SatisfiedForAudit()
			if !ready && coldChecks != nil {
				coldChecks.Add(1)
			}
			return ready
		},
	}))
	t.Cleanup(func() {
		select {
		case <-mgr.audit.stopper:
		case <-time.After(10 * time.Second):
			t.Error("audit loop did not stop")
		}
	})
	testutils.StartManager(t.Context(), t, mgr)
	return tracker
}
