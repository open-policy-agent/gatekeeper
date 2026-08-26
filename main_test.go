package main

import (
	"flag"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
)

// TestNewMutationSystem exercises the operation-dependent construction of
// the mutation system. The --operation flag can only accumulate values
// within a process, so the mutation-disabled case runs before the
// mutation-enabled cases.
func TestNewMutationSystem(t *testing.T) {
	// Non-mutation operation set: no mutation system is constructed.
	if err := flag.Set("operation", "audit"); err != nil {
		t.Fatalf("setting operation flag: %v", err)
	}
	if mutation.Enabled() {
		t.Fatalf("expected mutation to be disabled for audit-only operations")
	}
	if system := newMutationSystem(mutation.SystemOpts{}); system != nil {
		t.Errorf("newMutationSystem() = %v, want nil when no mutation operation is assigned", system)
	}

	// Adding a mutation operation constructs a real system.
	if err := flag.Set("operation", "mutation-controller"); err != nil {
		t.Fatalf("setting operation flag: %v", err)
	}
	if !mutation.Enabled() {
		t.Fatalf("expected mutation to be enabled once a mutation operation is assigned")
	}
	if system := newMutationSystem(mutation.SystemOpts{}); system == nil {
		t.Error("newMutationSystem() = nil, want a system when a mutation operation is assigned")
	}
}
