package match

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestApplyTo_MatchesOperation(t *testing.T) {
	tests := []struct {
		name      string
		applyTo   MutationApplyTo
		operation admissionv1.Operation
		want      bool
	}{
		{
			name:      "empty operation string - allows mutation",
			applyTo:   MutationApplyTo{},
			operation: "",
			want:      true,
		},
		{
			name:      "empty operations - allows CREATE (backward compatibility)",
			applyTo:   MutationApplyTo{},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name:      "empty operations - allows UPDATE (backward compatibility)",
			applyTo:   MutationApplyTo{},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name:      "empty operations - allows DELETE (backward compatibility)",
			applyTo:   MutationApplyTo{},
			operation: admissionv1.Delete,
			want:      true,
		},
		{
			name: "explicit CREATE only",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "explicit CREATE only - rejects UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			},
			operation: admissionv1.Update,
			want:      false,
		},
		{
			name: "explicit UPDATE only",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "explicit UPDATE only - rejects CREATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
			},
			operation: admissionv1.Create,
			want:      false,
		},
		{
			name: "multiple operations - CREATE and UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "multiple operations - CREATE and UPDATE with UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "multiple operations - rejects DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "DELETE operation allowed when explicitly specified",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete},
			},
			operation: admissionv1.Delete,
			want:      true,
		},
		{
			name: "multiple operations including DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update, admissionregistrationv1.Delete},
			},
			operation: admissionv1.Delete,
			want:      true,
		},
		{
			name: "CONNECT operation allowed when explicitly specified",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Connect},
			},
			operation: admissionv1.Connect,
			want:      true,
		},
		{
			name: "CONNECT operation rejected when not specified",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
			operation: admissionv1.Connect,
			want:      false,
		},
		{
			name: "OperationAll (*) matches CREATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "OperationAll (*) matches UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "OperationAll (*) matches DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Delete,
			want:      true,
		},
		{
			name: "OperationAll (*) matches CONNECT",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Connect,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.applyTo.MatchesOperation(tt.operation)
			if got != tt.want {
				t.Errorf("ApplyTo.MatchesOperation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyTo_Matches_WithOperations(t *testing.T) {
	tests := []struct {
		name    string
		applyTo MutationApplyTo
		gvk     schema.GroupVersionKind
		want    bool
	}{
		{
			name: "basic GVK match with operations field present",
			applyTo: MutationApplyTo{
				ApplyTo: ApplyTo{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				},
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, // This should not affect GVK matching
			},
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			want: true,
		},
		{
			name: "GVK no match with operations field present",
			applyTo: MutationApplyTo{
				ApplyTo: ApplyTo{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				},
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, // This should not affect GVK matching
			},
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Service", // Different kind
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.applyTo.Matches(tt.gvk)
			if got != tt.want {
				t.Errorf("ApplyTo.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateOperations(t *testing.T) {
	tests := []struct {
		name    string
		applyTo []MutationApplyTo
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty applyTo slice is valid",
			applyTo: []MutationApplyTo{},
			wantErr: false,
		},
		{
			name: "empty operations is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{}},
			},
			wantErr: false,
		},
		{
			name: "CREATE operation is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}},
			},
			wantErr: false,
		},
		{
			name: "UPDATE operation is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update}},
			},
			wantErr: false,
		},
		{
			name: "DELETE operation is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete}},
			},
			wantErr: false,
		},
		{
			name: "CONNECT operation is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Connect}},
			},
			wantErr: false,
		},
		{
			name: "OperationAll (*) is valid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll}},
			},
			wantErr: false,
		},
		{
			name: "all valid operations together",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
					admissionregistrationv1.Delete,
					admissionregistrationv1.Connect,
				}},
			},
			wantErr: false,
		},
		{
			name: "invalid operation is rejected",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{"INVALID"}},
			},
			wantErr: true,
			errMsg:  "invalid operation \"INVALID\"",
		},
		{
			name: "mixed valid and invalid operations - invalid rejected",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, "BOGUS"}},
			},
			wantErr: true,
			errMsg:  "invalid operation \"BOGUS\"",
		},
		{
			name: "invalid operation in second applyTo entry",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}},
				{Operations: []admissionregistrationv1.OperationType{"BAD_OP"}},
			},
			wantErr: true,
			errMsg:  "invalid operation \"BAD_OP\" in applyTo[1]",
		},
		{
			name: "lowercase operation is invalid",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{"create"}},
			},
			wantErr: true,
			errMsg:  "invalid operation \"create\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOperations(tt.applyTo)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOperations() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains([]string{err.Error()}, tt.errMsg) {
					// Check if error message contains expected substring
					if err != nil && !containsSubstring(err.Error(), tt.errMsg) {
						t.Errorf("ValidateOperations() error = %v, want error containing %q", err, tt.errMsg)
					}
				}
			}
		})
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
