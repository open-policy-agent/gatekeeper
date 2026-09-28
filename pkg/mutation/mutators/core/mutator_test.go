package core

import (
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func TestNewValidatedBindings_UnsupportedOperationsRejected(t *testing.T) {
	_, err := NewValidatedBindings("delete-connect-only", "Assign", []match.MutationApplyTo{
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
	if err == nil {
		t.Fatalf("NewValidatedBindings() error = <nil>, want rejection of unsupported operations")
	}
	if !strings.Contains(err.Error(), "DELETE") || !strings.Contains(err.Error(), "CONNECT") {
		t.Fatalf("NewValidatedBindings() error = %v, want it to name the rejected DELETE and CONNECT operations", err)
	}
}
