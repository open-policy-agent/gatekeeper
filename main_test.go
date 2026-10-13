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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var testConfig *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &testConfig, 0)
}

func TestSetupControllersStatusOnly(t *testing.T) {
	restoreOperations, err := operations.SetForTest(operations.Status)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restoreOperations)
	if got := operations.AssignedStringList(); len(got) != 1 || got[0] != string(operations.Status) {
		t.Fatalf("assigned operations = %v, want [%s]", got, operations.Status)
	}
	originalExternalDataEnabled := *externaldata.ExternalDataEnabled
	*externaldata.ExternalDataEnabled = false
	t.Cleanup(func() { *externaldata.ExternalDataEnabled = originalExternalDataEnabled })

	mgr, err := ctrl.NewManager(testConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		MapperProvider:         apiutil.NewDynamicRESTMapper,
		Logger:                 testutils.NewLogger(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := testutils.CreateGatekeeperNamespace(testConfig); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	constraintGVK := schema.GroupVersionKind{
		Group:   statusv1beta1.ConstraintsGroup,
		Version: "v1beta1",
		Kind:    "StatusOnlyConstraint",
	}
	constraintCRD := testConstraintCRD(constraintGVK)
	if err := mgr.GetClient().Create(ctx, constraintCRD); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.IgnoreNotFound(mgr.GetClient().Delete(context.Background(), constraintCRD)); err != nil {
			t.Error(err)
		}
	})
	constraintList := &unstructured.UnstructuredList{}
	constraintList.SetGroupVersionKind(constraintGVK.GroupVersion().WithKind(constraintGVK.Kind + "List"))
	if err := retry.OnError(testutils.ConstantRetry, func(error) bool { return true }, func() error {
		return mgr.GetClient().List(ctx, constraintList)
	}); err != nil {
		t.Fatalf("waiting for constraint CRD: %v", err)
	}

	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	setupFinished := make(chan struct{})
	close(setupFinished)
	if err := setupControllers(ctx, mgr, tracker, setupFinished); err != nil {
		t.Fatalf("setupControllers() = %v, want nil", err)
	}
	testutils.StartManager(ctx, t, mgr)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(cancel)
	if !mgr.GetCache().WaitForCacheSync(waitCtx) {
		t.Fatal("waiting for manager cache sync")
	}

	const templateName = "statusonlyconstraint"
	template := &templatesv1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: templateName},
		Spec: templatesv1beta1.ConstraintTemplateSpec{
			CRD: templatesv1beta1.CRD{Spec: templatesv1beta1.CRDSpec{
				Names: templatesv1beta1.Names{Kind: constraintGVK.Kind},
			}},
		},
	}
	if err := mgr.GetClient().Create(ctx, template); err != nil {
		t.Fatal(err)
	}
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(constraintGVK)
	constraint.SetName("status-only")
	if err := mgr.GetClient().Create(ctx, constraint); err != nil {
		t.Fatal(err)
	}

	templateStatus := &statusv1beta1.ConstraintTemplatePodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-only-template",
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				statusv1beta1.ConstraintTemplateNameLabel: templateName,
			},
		},
		Status: statusv1beta1.ConstraintTemplatePodStatusStatus{
			ID:          "status-only-pod",
			TemplateUID: template.GetUID(),
		},
	}
	if err := mgr.GetClient().Create(ctx, templateStatus); err != nil {
		t.Fatal(err)
	}
	constraintStatus := &statusv1beta1.ConstraintPodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-only-constraint",
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				statusv1beta1.ConstraintNameLabel: constraint.GetName(),
				statusv1beta1.ConstraintKindLabel: constraint.GetKind(),
			},
		},
		Status: statusv1beta1.ConstraintPodStatusStatus{
			ID:            "status-only-pod",
			ConstraintUID: constraint.GetUID(),
		},
	}
	if err := mgr.GetClient().Create(ctx, constraintStatus); err != nil {
		t.Fatal(err)
	}

	if err := retry.OnError(testutils.ConstantRetry, func(error) bool { return true }, func() error {
		got := &templatesv1beta1.ConstraintTemplate{}
		if err := mgr.GetClient().Get(ctx, types.NamespacedName{Name: templateName}, got); err != nil {
			return err
		}
		if len(got.Status.ByPod) != 1 || got.Status.ByPod[0].ID != "status-only-pod" {
			return fmt.Errorf("ConstraintTemplate status.byPod = %#v, want status-only-pod", got.Status.ByPod)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := retry.OnError(testutils.ConstantRetry, func(error) bool { return true }, func() error {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(constraintGVK)
		if err := mgr.GetClient().Get(ctx, types.NamespacedName{Name: constraint.GetName()}, got); err != nil {
			return err
		}
		byPod, _, err := unstructured.NestedSlice(got.Object, "status", "byPod")
		if err != nil {
			return err
		}
		if len(byPod) != 1 {
			return fmt.Errorf("Constraint status.byPod = %#v, want status-only-pod", byPod)
		}
		status, ok := byPod[0].(map[string]interface{})
		if !ok || status["id"] != "status-only-pod" {
			return fmt.Errorf("Constraint status.byPod = %#v, want status-only-pod", byPod)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testConstraintCRD(gvk schema.GroupVersionKind) *apiextensionsv1.CustomResourceDefinition {
	preserveUnknownFields := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "statusonlyconstraints." + gvk.Group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "statusonlyconstraints",
				Singular: "statusonlyconstraint",
				Kind:     gvk.Kind,
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    gvk.Version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						XPreserveUnknownFields: &preserveUnknownFields,
					},
				},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
			}},
		},
	}
}

func TestNewExportSystem(t *testing.T) {
	origExport := *exportutil.ExportEnabled
	origAdmission := *exportutil.AdmissionExportEnabled
	defer func() {
		*exportutil.ExportEnabled = origExport
		*exportutil.AdmissionExportEnabled = origAdmission
	}()

	tests := []struct {
		name             string
		exportEnabled    bool
		admissionEnabled bool
		wantNil          bool
	}{
		{"both disabled", false, false, true},
		{"audit export enabled", true, false, false},
		{"admission export enabled", false, true, false},
		{"both enabled", true, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			*exportutil.ExportEnabled = tc.exportEnabled
			*exportutil.AdmissionExportEnabled = tc.admissionEnabled

			got := newExportSystem()
			if gotNil := got == nil; gotNil != tc.wantNil {
				t.Errorf("newExportSystem() = %v, want nil: %v", got, tc.wantNil)
			}
		})
	}
}

// gatekeeperMainSubprocessEnvVar marks a re-exec'd child process that actually
// calls newMutationSystem. pkg/operations only allows the process-wide
// "operation" flag to be narrowed once (subsequent Set calls add to the
// existing set rather than replacing it), so each of the tests below runs its
// assertion in a fresh subprocess instead of sharing that global state, the
// same isolation approach used in
// pkg/controller/mutators/instances/mutator_controllers_test.go.
const gatekeeperMainSubprocessEnvVar = "GATEKEEPER_MAIN_SUBPROCESS"

// gatekeeperMainTestOperationEnvVar carries the desired --operation value into
// the subprocess.
const gatekeeperMainTestOperationEnvVar = "GATEKEEPER_MAIN_TEST_OPERATION"

func runNewMutationSystemInSubprocess(t *testing.T, testName, op string) (string, error) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$") //nolint:gosec // os.Args[0] is the current test binary; testName is always a compile-time string literal passed by the callers below
	cmd.Env = append(os.Environ(),
		gatekeeperMainSubprocessEnvVar+"=1",
		gatekeeperMainTestOperationEnvVar+"="+op,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestNewMutationSystemDisabledWhenNoMutationOperation guards against
// setupControllers unconditionally constructing a *mutation.System: when only
// a non-mutation operation (e.g. audit) is assigned, newMutationSystem must
// return nil.
func TestNewMutationSystemDisabledWhenNoMutationOperation(t *testing.T) {
	if os.Getenv(gatekeeperMainSubprocessEnvVar) == "1" {
		op := os.Getenv(gatekeeperMainTestOperationEnvVar)
		if err := flag.CommandLine.Set("operation", op); err != nil {
			t.Fatalf("setting operation flag to %q: %v", op, err)
		}

		if got := newMutationSystem(mutation.SystemOpts{}); got != nil {
			t.Fatalf("newMutationSystem() = %v, want nil for operation %q", got, op)
		}
		return
	}

	out, err := runNewMutationSystemInSubprocess(t, "TestNewMutationSystemDisabledWhenNoMutationOperation", string(operations.Audit))
	if err != nil {
		t.Fatalf("audit-only subprocess failed: %v\n%s", err, out)
	}
}

// TestNewMutationSystemEnabledWhenMutationOperationAssigned is the mutation-only
// counterpart: when a mutation operation is assigned, newMutationSystem must
// return a non-nil *mutation.System.
func TestNewMutationSystemEnabledWhenMutationOperationAssigned(t *testing.T) {
	if os.Getenv(gatekeeperMainSubprocessEnvVar) == "1" {
		op := os.Getenv(gatekeeperMainTestOperationEnvVar)
		if err := flag.CommandLine.Set("operation", op); err != nil {
			t.Fatalf("setting operation flag to %q: %v", op, err)
		}

		if got := newMutationSystem(mutation.SystemOpts{}); got == nil {
			t.Fatalf("newMutationSystem() = nil, want non-nil for operation %q", op)
		}
		return
	}

	out, err := runNewMutationSystemInSubprocess(t, "TestNewMutationSystemEnabledWhenMutationOperationAssigned", string(operations.MutationWebhook))
	if err != nil {
		t.Fatalf("mutation-webhook-only subprocess failed: %v\n%s", err, out)
	}
}

// Validates that newExpansionSystem only constructs a real expansion.System
// for processes that evaluate expanded resources (audit and webhook), per
// operations.HasValidationOperations. Other processes have no use for
// one, so setupControllers should not pay for its construction.
func Test_newExpansionSystem(t *testing.T) {
	tests := map[string]struct {
		assigned []operations.Operation
		wantNil  bool
	}{
		"audit only":            {assigned: []operations.Operation{operations.Audit}, wantNil: false},
		"webhook only":          {assigned: []operations.Operation{operations.Webhook}, wantNil: false},
		"audit and webhook":     {assigned: []operations.Operation{operations.Audit, operations.Webhook}, wantNil: false},
		"status only":           {assigned: []operations.Operation{operations.Status}, wantNil: true},
		"generate only":         {assigned: []operations.Operation{operations.Generate}, wantNil: true},
		"mutation-webhook only": {assigned: []operations.Operation{operations.MutationWebhook}, wantNil: true},
	}

	mutationSystem := mutation.NewSystem(mutation.SystemOpts{})

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			restore, err := operations.SetForTest(tc.assigned...)
			if err != nil {
				t.Fatal(err)
			}
			defer restore()

			got := newExpansionSystem(mutationSystem)
			if (got == nil) != tc.wantNil {
				t.Errorf("newExpansionSystem() = %v, wantNil %v", got, tc.wantNil)
			}
		})
	}
}
