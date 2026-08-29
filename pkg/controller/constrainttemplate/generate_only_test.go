/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package constrainttemplate

import (
	"context"
	"strings"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type nopReconciler struct{}

func (nopReconciler) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func setupUniqueNameManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	skipNameValidation := false
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Logger:         testutils.NewLogger(t),
		Controller:     ctrlconfig.Controller{SkipNameValidation: &skipNameValidation},
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		t.Fatalf("expected dynamic cache, got: %T", c)
	}
	wm, err := watch.New(dc)
	if err != nil {
		t.Fatalf("could not create watch manager: %s", err)
	}
	if err := mgr.Add(wm); err != nil {
		t.Fatalf("could not add watch manager to manager: %s", err)
	}
	return mgr, wm
}

func newGenerateOnlyClient(t *testing.T) *client.Client {
	t.Helper()
	regoDriver, err := rego.New(rego.Tracing(true))
	require.NoError(t, err)
	celDriver, err := k8scel.New()
	require.NoError(t, err)
	cfClient, err := client.NewClient(
		client.Targets(&target.K8sValidationTarget{}),
		client.Driver(regoDriver),
		client.Driver(celDriver),
		client.EnforcementPoints(operations.ConstraintClientEnforcementPoints()...),
	)
	require.NoError(t, err)
	return cfClient
}

func controllerRegistered(t *testing.T, mgr manager.Manager, name string) bool {
	t.Helper()
	_, err := controller.New(name, mgr, controller.Options{Reconciler: nopReconciler{}})
	return err != nil
}

// TestGenerateOnlyRegistersConstraintControllers fails if Adder.Add still
// uses HasValidationOperations() and therefore skips generate-only
// ConstraintTemplate/Constraint registration.
func TestGenerateOnlyRegistersConstraintControllers(t *testing.T) {
	operations.AssignForTest(t, operations.Generate)
	require.False(t, operations.HasValidationOperations(), "generate-only must not be treated as a validation/review operation")
	require.True(t, operations.HasConstraintControllers(), "generate-only must start constraint controllers")
	require.False(t, operations.IsAssigned(operations.Audit))
	require.False(t, operations.IsAssigned(operations.Webhook))
	require.False(t, operations.IsAssigned(operations.Status))
	require.Equal(t, []string{util.VAPEnforcementPoint}, operations.ConstraintClientEnforcementPoints())

	mgr, wm := setupUniqueNameManager(t)
	cfClient := newGenerateOnlyClient(t)
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)

	testutils.Setenv(t, "POD_NAME", "no-pod")
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	adder := &Adder{
		CFClient:     cfClient,
		WatchManager: wm,
		Tracker:      tracker,
		GetPod:       func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}
	require.NoError(t, adder.Add(mgr), "generate-only process must start successfully")

	if !controllerRegistered(t, mgr, ctrlName) {
		t.Fatal("constrainttemplate-controller was not registered; the old HasValidationOperations() gate is still blocking generate-only")
	}
	if !controllerRegistered(t, mgr, "constraint-controller") {
		t.Fatal("constraint-controller was not registered; generate-only must start Constraint reconciliation")
	}
	if controllerRegistered(t, mgr, "constraint-status-controller") {
		t.Fatal("generate-only must not register the constraint status aggregator")
	}
	if controllerRegistered(t, mgr, "constraint-template-status-controller") {
		t.Fatal("generate-only must not register the constraint template status aggregator")
	}
}

func TestAuditGenerateStillRegistersConstraintControllers(t *testing.T) {
	operations.AssignForTest(t, operations.Audit, operations.Generate)
	require.True(t, operations.HasValidationOperations())
	require.True(t, operations.HasConstraintControllers())
	require.Equal(t, []string{util.AuditEnforcementPoint}, operations.ConstraintClientEnforcementPoints())

	mgr, wm := setupUniqueNameManager(t)
	regoDriver, err := rego.New(rego.Tracing(true))
	require.NoError(t, err)
	celDriver, err := k8scel.New()
	require.NoError(t, err)
	cfClient, err := client.NewClient(
		client.Targets(&target.K8sValidationTarget{}),
		client.Driver(regoDriver),
		client.Driver(celDriver),
		client.EnforcementPoints(operations.ConstraintClientEnforcementPoints()...),
	)
	require.NoError(t, err)
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)

	testutils.Setenv(t, "POD_NAME", "no-pod")
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	require.NoError(t, (&Adder{
		CFClient:     cfClient,
		WatchManager: wm,
		Tracker:      tracker,
		GetPod:       func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}).Add(mgr))

	if !controllerRegistered(t, mgr, ctrlName) {
		t.Fatal("audit+generate must still register constrainttemplate-controller")
	}
	if !controllerRegistered(t, mgr, "constraint-controller") {
		t.Fatal("audit+generate must still register constraint-controller")
	}
}

func TestWebhookGenerateStillRegistersConstraintControllers(t *testing.T) {
	operations.AssignForTest(t, operations.Webhook, operations.Generate)
	require.True(t, operations.HasValidationOperations())
	require.True(t, operations.HasConstraintControllers())
	require.Equal(t, []string{util.WebhookEnforcementPoint}, operations.ConstraintClientEnforcementPoints())

	mgr, wm := setupUniqueNameManager(t)
	regoDriver, err := rego.New(rego.Tracing(true))
	require.NoError(t, err)
	celDriver, err := k8scel.New()
	require.NoError(t, err)
	cfClient, err := client.NewClient(
		client.Targets(&target.K8sValidationTarget{}),
		client.Driver(regoDriver),
		client.Driver(celDriver),
		client.EnforcementPoints(operations.ConstraintClientEnforcementPoints()...),
	)
	require.NoError(t, err)
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)

	testutils.Setenv(t, "POD_NAME", "no-pod")
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	require.NoError(t, (&Adder{
		CFClient:     cfClient,
		WatchManager: wm,
		Tracker:      tracker,
		GetPod:       func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}).Add(mgr))

	if !controllerRegistered(t, mgr, ctrlName) {
		t.Fatal("webhook+generate must still register constrainttemplate-controller")
	}
	if !controllerRegistered(t, mgr, "constraint-controller") {
		t.Fatal("webhook+generate must still register constraint-controller")
	}
}

// TestGenerateOnlyReconcilesConstraintCRD applies a CEL ConstraintTemplate in
// generate-only mode and waits for the Constraint CRD (and VAP when the API
// is available).
func TestGenerateOnlyReconcilesConstraintCRD(t *testing.T) {
	operations.AssignForTest(t, operations.Generate)

	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())
	require.NoError(t, testutils.CreateGatekeeperNamespace(mgr.GetConfig()))

	cfClient := newGenerateOnlyClient(t)
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)

	testutils.Setenv(t, "POD_NAME", "no-pod")
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	require.NoError(t, (&Adder{
		CFClient:     cfClient,
		WatchManager: wm,
		Tracker:      tracker,
		GetPod:       func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}).Add(mgr))

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	origWait := constraint.GetDefaultWaitForVAPBGeneration()
	constraint.SetDefaultWaitForVAPBGeneration(2)
	t.Cleanup(func() { constraint.SetDefaultWaitForVAPBGeneration(origWait) })

	suffix := "GenerateOnlyCRD"
	ct := makeReconcileConstraintTemplateForVap(suffix, nil, nil)
	t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
	testutils.CreateThenCleanup(ctx, t, c, ct)

	err = retry.OnError(testutils.ConstantRetry, func(_ error) bool { return true }, func() error {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		return c.Get(ctx, crdKey(suffix), crd)
	})
	require.NoError(t, err, "generate-only must reconcile the Constraint CRD")

	cstr := newDenyAllCstr(suffix)
	testutils.CreateThenCleanup(ctx, t, c, cstr)

	err = retry.OnError(testutils.ConstantRetry, func(_ error) bool { return true }, func() error {
		got := cstr.DeepCopy()
		if getErr := c.Get(ctx, types.NamespacedName{Name: cstr.GetName()}, got); getErr != nil {
			return getErr
		}
		return nil
	})
	require.NoError(t, err, "generate-only must be able to persist a matching Constraint")

	vapName := types.NamespacedName{Name: "gatekeeper-" + denyall + strings.ToLower(suffix)}
	vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	if probeErr := c.Get(ctx, vapName, vap); meta.IsNoMatchError(probeErr) {
		t.Logf("skipping VAP assertion; VAP API is not available in this envtest: %v", probeErr)
		return
	}
	err = retry.OnError(testutils.ConstantRetry, func(_ error) bool { return true }, func() error {
		return c.Get(ctx, vapName, vap)
	})
	require.NoError(t, err, "generate-only must reconcile VAP when the ValidatingAdmissionPolicy API is available")
}
