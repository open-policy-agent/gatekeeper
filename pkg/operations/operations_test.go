package operations

import (
	"flag"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
)

// Validates flags parsing for operations.
func Test_Flags(t *testing.T) {
	tests := map[string]struct {
		input    []string
		expected map[Operation]bool
	}{
		"default": {
			input:    []string{},
			expected: map[Operation]bool{Audit: true, Webhook: true, Status: true, MutationStatus: true, MutationWebhook: true, MutationController: true, Generate: true},
		},
		"multiple": {
			input:    []string{"-operation", "audit", "-operation", "webhook"},
			expected: map[Operation]bool{Audit: true, Webhook: true},
		},
		"split": {
			input:    []string{"-operation", "audit,status"},
			expected: map[Operation]bool{Audit: true, Status: true},
		},
		"both": {
			input:    []string{"-operation", "audit,status", "-operation", "webhook"},
			expected: map[Operation]bool{Audit: true, Status: true, Webhook: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ops := newOperationSet()
			flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
			flagSet.Var(ops, "operation", "The operation to be performed by this instance. e.g. audit, webhook. This flag can be declared more than once. Omitting will default to supporting all operations.")

			err := flagSet.Parse(tc.input)
			if err != nil {
				t.Errorf("parsing: %v", err)
				return
			}
			if diff := cmp.Diff(tc.expected, ops.assignedOperations); diff != "" {
				t.Errorf("unexpected result: %s", diff)
			}
		})
	}
}

func TestOperationCapabilities(t *testing.T) {
	tests := []struct {
		name                      string
		assigned                  []Operation
		wantValidation            bool
		wantConstraintControllers bool
		wantAudit                 bool
		wantWebhook               bool
		wantStatus                bool
		wantEnforcementPoints     []string
	}{
		{
			name:                      "generate only",
			assigned:                  []Operation{Generate},
			wantConstraintControllers: true,
			wantEnforcementPoints:     []string{util.VAPEnforcementPoint},
		},
		{
			name:                      "audit only",
			assigned:                  []Operation{Audit},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantAudit:                 true,
			wantEnforcementPoints:     []string{util.AuditEnforcementPoint},
		},
		{
			name:                      "webhook only",
			assigned:                  []Operation{Webhook},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantWebhook:               true,
			wantEnforcementPoints:     []string{util.WebhookEnforcementPoint},
		},
		{
			name:                      "status only",
			assigned:                  []Operation{Status},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantStatus:                true,
		},
		{
			name:                      "audit and generate",
			assigned:                  []Operation{Audit, Generate},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantAudit:                 true,
			wantEnforcementPoints:     []string{util.AuditEnforcementPoint},
		},
		{
			name:                      "webhook and generate",
			assigned:                  []Operation{Webhook, Generate},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantWebhook:               true,
			wantEnforcementPoints:     []string{util.WebhookEnforcementPoint},
		},
		{
			name:                      "audit webhook and generate",
			assigned:                  []Operation{Audit, Webhook, Generate},
			wantValidation:            true,
			wantConstraintControllers: true,
			wantAudit:                 true,
			wantWebhook:               true,
			wantEnforcementPoints:     []string{util.AuditEnforcementPoint, util.WebhookEnforcementPoint},
		},
		{
			name:     "mutation webhook only",
			assigned: []Operation{MutationWebhook},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			AssignForTest(t, tc.assigned...)

			if got := HasValidationOperations(); got != tc.wantValidation {
				t.Errorf("HasValidationOperations() = %t, want %t", got, tc.wantValidation)
			}
			if got := HasConstraintControllers(); got != tc.wantConstraintControllers {
				t.Errorf("HasConstraintControllers() = %t, want %t", got, tc.wantConstraintControllers)
			}
			if got := IsAssigned(Audit); got != tc.wantAudit {
				t.Errorf("IsAssigned(Audit) = %t, want %t", got, tc.wantAudit)
			}
			if got := IsAssigned(Webhook); got != tc.wantWebhook {
				t.Errorf("IsAssigned(Webhook) = %t, want %t", got, tc.wantWebhook)
			}
			if got := IsAssigned(Status); got != tc.wantStatus {
				t.Errorf("IsAssigned(Status) = %t, want %t", got, tc.wantStatus)
			}
			if diff := cmp.Diff(tc.wantEnforcementPoints, ConstraintClientEnforcementPoints()); diff != "" {
				t.Errorf("ConstraintClientEnforcementPoints() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestHasConstraintControllers_GenerateOnlyFailsIfOldGateRestored documents
// that generate-only must not be classified as a validation/review operation.
// Restoring HasValidationOperations() as the ConstraintTemplate/Constraint
// adder gate makes this assertion fail because generate-only is false there.
func TestHasConstraintControllers_GenerateOnlyFailsIfOldGateRestored(t *testing.T) {
	AssignForTest(t, Generate)

	if HasValidationOperations() {
		t.Fatal("HasValidationOperations() is true for generate-only; generate must stay independent of audit/status/webhook")
	}
	if !HasConstraintControllers() {
		t.Fatal("HasConstraintControllers() is false for generate-only; the old HasValidationOperations() gate would skip CRD/VAP/VAPB reconcilers")
	}
	if IsAssigned(Audit) || IsAssigned(Webhook) || IsAssigned(Status) {
		t.Fatal("generate-only must not implicitly assign audit, webhook, or status")
	}
	got := ConstraintClientEnforcementPoints()
	if diff := cmp.Diff([]string{util.VAPEnforcementPoint}, got); diff != "" {
		t.Errorf("generate-only must not model generation as audit/webhook enforcement points (-want +got):\n%s", diff)
	}
}
