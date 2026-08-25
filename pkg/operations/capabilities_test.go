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

package operations

import "testing"

func TestCapabilities(t *testing.T) {
	tests := []struct {
		name                  string
		assigned              []Operation
		dataSync              bool
		processExclusions     bool
		generateNotifications bool
		configReconciliation  bool
	}{
		{
			name:                 "no operations",
			assigned:             nil,
			configReconciliation: false,
		},
		{
			name:                 "audit",
			assigned:             []Operation{Audit},
			dataSync:             true,
			processExclusions:    true,
			configReconciliation: true,
		},
		{
			name:                 "validating webhook",
			assigned:             []Operation{Webhook},
			dataSync:             true,
			processExclusions:    true,
			configReconciliation: true,
		},
		{
			name:                 "mutation webhook only",
			assigned:             []Operation{MutationWebhook},
			processExclusions:    true,
			configReconciliation: true,
		},
		{
			name:                 "mutation status only",
			assigned:             []Operation{MutationStatus},
			configReconciliation: false,
		},
		{
			name:                 "mutation controller only",
			assigned:             []Operation{MutationController},
			configReconciliation: false,
		},
		{
			name:                  "generate only",
			assigned:              []Operation{Generate},
			processExclusions:     true,
			generateNotifications: true,
			configReconciliation:  true,
		},
		{
			name:                  "all operations",
			assigned:              allOperations,
			dataSync:              true,
			processExclusions:     true,
			generateNotifications: true,
			configReconciliation:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(AssignForTest(tt.assigned...))

			if got := NeedsValidationDataSync(); got != tt.dataSync {
				t.Errorf("NeedsValidationDataSync() = %t, want %t", got, tt.dataSync)
			}
			if got := NeedsProcessExclusions(); got != tt.processExclusions {
				t.Errorf("NeedsProcessExclusions() = %t, want %t", got, tt.processExclusions)
			}
			if got := NeedsGenerateConfigNotifications(); got != tt.generateNotifications {
				t.Errorf("NeedsGenerateConfigNotifications() = %t, want %t", got, tt.generateNotifications)
			}
			if got := NeedsConfigReconciliation(); got != tt.configReconciliation {
				t.Errorf("NeedsConfigReconciliation() = %t, want %t", got, tt.configReconciliation)
			}
		})
	}
}

func TestAssignForTestRestores(t *testing.T) {
	before := AssignedStringList()

	restore := AssignForTest(Audit)
	if got := AssignedStringList(); len(got) != 1 || got[0] != string(Audit) {
		t.Fatalf("AssignedStringList() = %v, want [audit]", got)
	}
	restore()

	after := AssignedStringList()
	if len(before) != len(after) {
		t.Fatalf("AssignedStringList() = %v, want %v", after, before)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("AssignedStringList() = %v, want %v", after, before)
		}
	}
}
