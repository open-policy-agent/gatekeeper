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
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// runnableCounter counts the runnables, such as controllers, registered with
// the wrapped manager.
type runnableCounter struct {
	manager.Manager
	added int
}

func (m *runnableCounter) Add(r manager.Runnable) error {
	m.added++
	return m.Manager.Add(r)
}

// Pods without audit or webhook skip template ingestion and only call
// addStatusControllers, which relies on each status controller checking for
// the status operation itself.
func TestAddRegistersStatusControllersOnlyWithStatusOperation(t *testing.T) {
	tests := []struct {
		name string
		ops  []operations.Operation
		want int
	}{
		{name: "generate only", ops: []operations.Operation{operations.Generate}, want: 0},
		{name: "mutation-status only", ops: []operations.Operation{operations.MutationStatus}, want: 0},
		{name: "status only", ops: []operations.Operation{operations.Status}, want: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			globalTestMu.Lock()
			defer globalTestMu.Unlock()

			restore, err := operations.SetForTest(tc.ops...)
			if err != nil {
				t.Fatalf("setting operations %v: %v", tc.ops, err)
			}
			defer restore()

			mgr, _ := testutils.SetupManager(t, cfg)
			counter := &runnableCounter{Manager: mgr}
			if err := (&Adder{}).Add(counter); err != nil {
				t.Fatalf("Add() = %v, want nil", err)
			}
			if counter.added != tc.want {
				t.Errorf("Add() registered %d controllers with operations %v, want %d", counter.added, tc.ops, tc.want)
			}
		})
	}
}
