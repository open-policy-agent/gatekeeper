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

func Test_HasExpansionEvaluationOperations(t *testing.T) {
	tests := map[string]struct {
		input    []string
		expected bool
	}{
		"default":                   {input: []string{}, expected: true},
		"audit only":                {input: []string{"-operation", "audit"}, expected: true},
		"webhook only":              {input: []string{"-operation", "webhook"}, expected: true},
		"status only":               {input: []string{"-operation", "status"}, expected: false},
		"generate only":             {input: []string{"-operation", "generate"}, expected: false},
		"mutation only":             {input: []string{"-operation", "mutation-webhook,mutation-controller,mutation-status"}, expected: false},
		"audit+status+mut+generate": {input: []string{"-operation", "audit,status,mutation-status,generate"}, expected: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ResetForTest()
			t.Cleanup(ResetForTest)

			flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
			flagSet.Var(operations, "operation", "test operation flag")
			if err := flagSet.Parse(tc.input); err != nil {
				t.Fatalf("parsing: %v", err)
			}

			if got := HasExpansionEvaluationOperations(); got != tc.expected {
				t.Errorf("HasExpansionEvaluationOperations() = %v, want %v", got, tc.expected)
			}
		})
	}
}
