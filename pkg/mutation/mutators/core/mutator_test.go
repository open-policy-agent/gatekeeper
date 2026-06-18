package core

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func TestNewValidatedBindings_AllUnsupportedOperationsAllowedWithoutBindings(t *testing.T) {
	bindings, err := NewValidatedBindings("delete-connect-only", "Assign", []match.MutationApplyTo{
		{
			ApplyTo: match.ApplyTo{
				Groups:   []string{""},
				Versions: []string{"v1"},
				Kinds:    []string{"Pod"},
			},
			Operations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Delete,
				admissionregistrationv1.Connect,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewValidatedBindings() error = %v, want <nil>", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("NewValidatedBindings() returned %d bindings, want 0", len(bindings))
	}
}
