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
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
)

// Validates that newExpansionSystem only constructs a real expansion.System
// for processes that evaluate expanded resources (audit and webhook), per
// operations.HasExpansionConsumerOperations. Other processes have no use for
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
			restore := operations.AssignForTest(tc.assigned...)
			defer restore()

			got := newExpansionSystem(mutationSystem)
			if (got == nil) != tc.wantNil {
				t.Errorf("newExpansionSystem() = %v, wantNil %v", got, tc.wantNil)
			}
		})
	}
}
