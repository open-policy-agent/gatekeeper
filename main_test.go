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
	"flag"
	"os"
	"os/exec"
	"testing"

	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
)

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

	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
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
