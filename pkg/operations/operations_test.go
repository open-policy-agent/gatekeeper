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

func TestHasValidationOperations(t *testing.T) {
	original := operations
	t.Cleanup(func() {
		operationsMtx.Lock()
		defer operationsMtx.Unlock()
		operations = original
	})

	tests := map[string]struct {
		assigned []Operation
		want     bool
	}{
		"status only":         {assigned: []Operation{Status}},
		"generate only":       {assigned: []Operation{Generate}},
		"audit":               {assigned: []Operation{Audit}, want: true},
		"webhook":             {assigned: []Operation{Webhook}, want: true},
		"status with audit":   {assigned: []Operation{Status, Audit}, want: true},
		"status with webhook": {assigned: []Operation{Status, Webhook}, want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assigned := newOperationSet()
			assigned.assignedOperations = make(map[Operation]bool)
			for _, op := range tc.assigned {
				assigned.assignedOperations[op] = true
			}

			operationsMtx.Lock()
			operations = assigned
			operationsMtx.Unlock()

			if got := HasValidationOperations(); got != tc.want {
				t.Errorf("HasValidationOperations() = %t, want %t", got, tc.want)
			}
		})
	}
}
