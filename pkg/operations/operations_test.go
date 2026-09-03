package operations

import (
	"flag"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// Validates HasExpansionConsumerOperations only reports true for the
// operations that actually evaluate expanded resources.
func Test_HasExpansionConsumerOperations(t *testing.T) {
	tests := map[string]struct {
		assigned map[Operation]bool
		expected bool
	}{
		"audit only":            {assigned: map[Operation]bool{Audit: true}, expected: true},
		"webhook only":          {assigned: map[Operation]bool{Webhook: true}, expected: true},
		"audit and webhook":     {assigned: map[Operation]bool{Audit: true, Webhook: true}, expected: true},
		"status only":           {assigned: map[Operation]bool{Status: true}, expected: false},
		"generate only":         {assigned: map[Operation]bool{Generate: true}, expected: false},
		"mutation-status only":  {assigned: map[Operation]bool{MutationStatus: true}, expected: false},
		"mutation-webhook only": {assigned: map[Operation]bool{MutationWebhook: true}, expected: false},
		"none assigned":         {assigned: map[Operation]bool{}, expected: false},
	}

	original := operations
	defer func() { operations = original }()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			operations = &opSet{assignedOperations: tc.assigned}
			if got := HasExpansionConsumerOperations(); got != tc.expected {
				t.Errorf("HasExpansionConsumerOperations() = %v, want %v", got, tc.expected)
			}
		})
	}
}
