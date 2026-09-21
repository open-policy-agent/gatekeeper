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
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

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

func adderProceededPastOperationGate(t *testing.T) bool {
	t.Helper()
	proceeded := false
	func() {
		defer func() {
			if recover() != nil {
				proceeded = true
			}
		}()
		err := (&Adder{}).Add(nil)
		if err != nil {
			proceeded = true
		}
	}()
	return proceeded
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

	if !adderProceededPastOperationGate(t) {
		t.Fatal("generate-only Adder.Add returned nil without touching the manager; the old HasValidationOperations() gate is still blocking registration")
	}
}

func TestAuditGenerateStillRegistersConstraintControllers(t *testing.T) {
	operations.AssignForTest(t, operations.Audit, operations.Generate)
	require.True(t, operations.HasValidationOperations())
	require.True(t, operations.HasConstraintControllers())
	require.Equal(t, []string{util.AuditEnforcementPoint}, operations.ConstraintClientEnforcementPoints())
	if !adderProceededPastOperationGate(t) {
		t.Fatal("audit+generate must still register ConstraintTemplate/Constraint controllers")
	}
}

func TestWebhookGenerateStillRegistersConstraintControllers(t *testing.T) {
	operations.AssignForTest(t, operations.Webhook, operations.Generate)
	require.True(t, operations.HasValidationOperations())
	require.True(t, operations.HasConstraintControllers())
	require.Equal(t, []string{util.WebhookEnforcementPoint}, operations.ConstraintClientEnforcementPoints())
	if !adderProceededPastOperationGate(t) {
		t.Fatal("webhook+generate must still register ConstraintTemplate/Constraint controllers")
	}
}

func TestStatusOnlyDoesNotImplicitlyBecomeGenerate(t *testing.T) {
	operations.AssignForTest(t, operations.Status)
	require.True(t, operations.HasValidationOperations())
	require.True(t, operations.HasConstraintControllers())
	require.False(t, operations.IsAssigned(operations.Generate), "status-only must not implicitly become a generate pod")
	require.Empty(t, operations.ConstraintClientEnforcementPoints())
}

// TestGenerateOnlyReconcilesConstraintCRD applies a CEL ConstraintTemplate in
// generate-only mode and waits for the Constraint CRD (and VAP when the API
// is available).
func TestGenerateOnlyReconcilesConstraintCRD(t *testing.T) {
	operations.AssignForTest(t, operations.Generate)
	setVAPTestGlobals(t, &schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: "v1"})

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
	}).Add(mgr), "generate-only process must start successfully")

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	origWait := constraint.GetDefaultWaitForVAPBGeneration()
	constraint.SetDefaultWaitForVAPBGeneration(2)
	t.Cleanup(func() { constraint.SetDefaultWaitForVAPBGeneration(origWait) })

	suffix := "GenerateOnlyCRD"
	ct := makeReconcileConstraintTemplateForVap(suffix, ptr.To(true), nil)
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
		return c.Get(ctx, types.NamespacedName{Name: cstr.GetName()}, got)
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
