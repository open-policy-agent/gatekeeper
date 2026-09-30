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

package constraint

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
)

// TestAdderGenerateOnlyDoesNotSkip fails if Adder.Add still uses
// HasValidationOperations() and therefore returns without registering
// the Constraint reconciler for --operation=generate.
func TestAdderGenerateOnlyDoesNotSkip(t *testing.T) {
	operations.AssignForTest(t, operations.Generate)

	if operations.HasValidationOperations() {
		t.Fatal("HasValidationOperations() is true for generate-only; the old adder gate would skip Constraint reconciliation")
	}
	if !operations.HasConstraintControllers() {
		t.Fatal("HasConstraintControllers() is false for generate-only")
	}

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
	if !proceeded {
		t.Fatal("generate-only Adder.Add returned nil without touching the manager; the old HasValidationOperations() gate is still blocking registration")
	}
}

func TestAdderAuditGenerateDoesNotSkip(t *testing.T) {
	operations.AssignForTest(t, operations.Audit, operations.Generate)
	if !operations.HasValidationOperations() || !operations.HasConstraintControllers() {
		t.Fatal("audit+generate must still register constraint controllers")
	}
}

func TestAdderWebhookGenerateDoesNotSkip(t *testing.T) {
	operations.AssignForTest(t, operations.Webhook, operations.Generate)
	if !operations.HasValidationOperations() || !operations.HasConstraintControllers() {
		t.Fatal("webhook+generate must still register constraint controllers")
	}
}
