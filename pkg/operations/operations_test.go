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

// Test_HelperFunctions validates the helper functions for checking operation types.
func Test_HelperFunctions(t *testing.T) {
	tests := []struct {
		name                 string
		operations           []string
		expectValidation     bool
		expectMutation       bool
		expectStatus         bool
		expectGenerate       bool
	}{
		{
			name:                 "audit only",
			operations:           []string{"audit"},
			expectValidation:     true,
			expectMutation:       false,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "webhook only",
			operations:           []string{"webhook"},
			expectValidation:     true,
			expectMutation:       false,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "status only",
			operations:           []string{"status"},
			expectValidation:     true,
			expectMutation:       false,
			expectStatus:         true,
			expectGenerate:       false,
		},
		{
			name:                 "mutation-webhook only",
			operations:           []string{"mutation-webhook"},
			expectValidation:     false,
			expectMutation:       true,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "mutation-controller only",
			operations:           []string{"mutation-controller"},
			expectValidation:     false,
			expectMutation:       true,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "mutation-status only",
			operations:           []string{"mutation-status"},
			expectValidation:     false,
			expectMutation:       true,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "generate only",
			operations:           []string{"generate"},
			expectValidation:     false,
			expectMutation:       false,
			expectStatus:         false,
			expectGenerate:       true,
		},
		{
			name:                 "audit and mutation-webhook",
			operations:           []string{"audit", "mutation-webhook"},
			expectValidation:     true,
			expectMutation:       true,
			expectStatus:         false,
			expectGenerate:       false,
		},
		{
			name:                 "status and generate",
			operations:           []string{"status", "generate"},
			expectValidation:     true,
			expectMutation:       false,
			expectStatus:         true,
			expectGenerate:       true,
		},
		{
			name:                 "all operations",
			operations:           []string{"audit", "webhook", "status", "mutation-webhook", "mutation-controller", "mutation-status", "generate"},
			expectValidation:     true,
			expectMutation:       true,
			expectStatus:         true,
			expectGenerate:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save current state
			operationsMtx.Lock()
			oldOps := operations
			operations = newOperationSet()
			operationsMtx.Unlock()

			// Set up test operations
			flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
			flagSet.Var(operations, "operation", "test")
			
			args := []string{}
			for _, op := range tt.operations {
				args = append(args, "-operation", op)
			}
			
			if err := flagSet.Parse(args); err != nil {
				t.Fatalf("failed to parse flags: %v", err)
			}

			// Test helper functions
			if got := HasValidationOperations(); got != tt.expectValidation {
				t.Errorf("HasValidationOperations() = %v, want %v", got, tt.expectValidation)
			}
			if got := HasMutationOperations(); got != tt.expectMutation {
				t.Errorf("HasMutationOperations() = %v, want %v", got, tt.expectMutation)
			}
			if got := HasStatusOperation(); got != tt.expectStatus {
				t.Errorf("HasStatusOperation() = %v, want %v", got, tt.expectStatus)
			}
			if got := HasGenerateOperation(); got != tt.expectGenerate {
				t.Errorf("HasGenerateOperation() = %v, want %v", got, tt.expectGenerate)
			}

			// Restore state
			operationsMtx.Lock()
			operations = oldOps
			operationsMtx.Unlock()
		})
	}
}

