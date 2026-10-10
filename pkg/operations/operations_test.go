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

func TestSetForTest(t *testing.T) {
	original := operations
	restore, err := SetForTest(Status)
	if err != nil {
		t.Fatal(err)
	}
	if got := AssignedStringList(); len(got) != 1 || got[0] != string(Status) {
		t.Fatalf("AssignedStringList() = %v, want [%s]", got, Status)
	}

	restore()
	if operations != original {
		t.Fatal("SetForTest restore did not restore the original operations")
	}
}

// Validates HasExpansionConsumerOperations only reports true for the
// operations that actually evaluate expanded resources.
func Test_HasExpansionConsumerOperations(t *testing.T) {
	tests := map[string]struct {
		assigned []Operation
		expected bool
	}{
		"audit only":               {assigned: []Operation{Audit}, expected: true},
		"webhook only":             {assigned: []Operation{Webhook}, expected: true},
		"audit and webhook":        {assigned: []Operation{Audit, Webhook}, expected: true},
		"status only":              {assigned: []Operation{Status}, expected: false},
		"generate only":            {assigned: []Operation{Generate}, expected: false},
		"mutation-controller only": {assigned: []Operation{MutationController}, expected: false},
		"mutation-status only":     {assigned: []Operation{MutationStatus}, expected: false},
		"mutation-webhook only":    {assigned: []Operation{MutationWebhook}, expected: false},
		"none assigned":            {assigned: []Operation{}, expected: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			restore := AssignForTest(tc.assigned...)
			defer restore()

			if got := HasExpansionConsumerOperations(); got != tc.expected {
				t.Errorf("HasExpansionConsumerOperations() = %v, want %v", got, tc.expected)
			}
		})
	}
}
