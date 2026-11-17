package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Exercise the real controller and audit loop, not a runner with a stub audit callback.
func TestAuditTriggerProductionFailures(t *testing.T) {
	oldInterval, oldMatchKindOnly, oldChunkSize := *auditInterval, *auditMatchKindOnly, *auditChunkSize
	oldFromCache, oldExport := *auditFromCache, *exportutil.ExportEnabled
	restore := setAuditGlobalsForReviewTest(t)
	t.Cleanup(func() {
		restore()
		*auditInterval, *auditMatchKindOnly, *auditChunkSize = oldInterval, oldMatchKindOnly, oldChunkSize
		*auditFromCache, *exportutil.ExportEnabled = oldFromCache, oldExport
	})
	*auditInterval, *auditMatchKindOnly, *auditChunkSize = 0, true, 1
	*auditFromCache = false
	*apiCacheDir = t.TempDir()

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

	faults := &atomic.Pointer[auditHTTPFault]{}
	managerConfig := rest.CopyConfig(cfg)
	managerConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &auditFaultTransport{RoundTripper: rt, fault: faults}
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
	require.NoError(t, AddToManager(mgr, &Dependencies{
		Client:          opa,
		ProcessExcluder: process.New(),
		ExpansionSystem: expansion.NewSystem(nil),
	}))
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

	constraintPath := "/apis/" + constraintsGV + "/" + versionedCRD.Spec.Names.Plural
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
				return r.URL.Path == constraintPath && r.URL.Query().Get("limit") == ""
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
				// Skip constraint discovery and REST mapping; fail both full-discovery attempts.
				fault.skip, fault.count = 2, 2
			}
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("last trigger status: %+v; matching requests: %d; injected faults: %d", trigger.Status, fault.seen.Load(), fault.hits.Load())
				}
			})
			faults.Store(fault)
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
}

func (m *auditRecordingManager) Add(r manager.Runnable) error {
	if am, ok := r.(*Manager); ok {
		m.audit = am
	}
	return m.Manager.Add(r)
}

type auditHTTPFault struct {
	match func(*http.Request) bool
	code  int
	skip  int32
	count int32
	seen  atomic.Int32
	hits  atomic.Int32
}

type auditFaultTransport struct {
	http.RoundTripper
	fault *atomic.Pointer[auditHTTPFault]
}

func (rt *auditFaultTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fault := rt.fault.Load()
	if fault != nil && fault.skip > 0 && (req.URL.Path == "/api" || req.URL.Path == "/apis") {
		// Exercise the supported legacy partial-discovery path on servers with aggregated discovery.
		req = req.Clone(req.Context())
		req.Header.Set("Accept", "application/json")
	}
	if fault == nil || req.Method != http.MethodGet || !fault.match(req) ||
		fault.seen.Add(1) <= fault.skip {
		return rt.RoundTripper.RoundTrip(req)
	}
	if fault.hits.Add(1) == fault.count {
		rt.fault.CompareAndSwap(fault, nil)
	}
	status := apierrors.NewServiceUnavailable("injected audit failure").ErrStatus
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
