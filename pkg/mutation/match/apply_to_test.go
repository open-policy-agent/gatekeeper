package match

import (
	"slices"
	"strings"
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
			name:      "empty operation string with empty operations - allows mutation",
			applyTo:   MutationApplyTo{},
			operation: "",
			want:      true,
		},
		{
			name: "empty operation string with explicit CREATE - rejects mutation",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			},
			operation: "",
			want:      false,
		},
		{
			name: "empty operation string with OperationAll - allows mutation",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: "",
			want:      true,
		},
		{
			name: "empty operation string with all supported operations - allows mutation",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
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
			name:      "empty operations - rejects unsupported DELETE",
			applyTo:   MutationApplyTo{},
			operation: admissionv1.Delete,
			want:      false,
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
			name: "DELETE operation does not match until mutation webhook supports it",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "multiple operations including DELETE rejects DELETE until mutation webhook supports it",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update, admissionregistrationv1.Delete},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "CONNECT operation does not match until mutation webhook supports it",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Connect},
			},
			operation: admissionv1.Connect,
			want:      false,
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
			name: "OperationAll (*) rejects unsupported DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "OperationAll (*) rejects unsupported CONNECT",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			operation: admissionv1.Connect,
			want:      false,
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

func TestApplyTo_EffectiveOperations(t *testing.T) {
	tests := []struct {
		name    string
		applyTo MutationApplyTo
		want    []admissionv1.Operation
	}{
		{
			name:    "empty operations only bind supported mutation operations",
			applyTo: MutationApplyTo{},
			want:    []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
		},
		{
			name: "OperationAll only binds supported mutation operations",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
			},
			want: []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
		},
		{
			name: "CREATE and UPDATE are preserved",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			},
			want: []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
		},
		{
			name: "DELETE and CONNECT do not create schema conflict bindings",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Delete, admissionregistrationv1.Connect},
			},
			want: nil,
		},
		{
			name: "unsupported operations are filtered from mixed operation lists",
			applyTo: MutationApplyTo{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Delete, admissionregistrationv1.Connect},
			},
			want: []admissionv1.Operation{admissionv1.Create},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.applyTo.EffectiveOperations()
			if !slices.Equal(got, tt.want) {
				t.Errorf("EffectiveOperations() = %v, want %v", got, tt.want)
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

func TestAppliesGVKAndOperation(t *testing.T) {
	podGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	deployGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}

	tests := []struct {
		name      string
		applyTo   []MutationApplyTo
		gvk       schema.GroupVersionKind
		operation admissionv1.Operation
		want      bool
	}{
		{
			name: "single entry matches both GVK and operation",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				},
			},
			gvk:       podGVK,
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "single entry matches GVK but not operation",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				},
			},
			gvk:       podGVK,
			operation: admissionv1.Update,
			want:      false,
		},
		{
			name: "cross-entry false positive: GVK matches entry[0], operation matches entry[1]",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				},
				{
					ApplyTo:    ApplyTo{Groups: []string{"apps"}, Kinds: []string{"Deployment"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				},
			},
			gvk:       podGVK,
			operation: admissionv1.Update,
			want:      false,
		},
		{
			name: "cross-entry: correct entry matches both",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				},
				{
					ApplyTo:    ApplyTo{Groups: []string{"apps"}, Kinds: []string{"Deployment"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				},
			},
			gvk:       deployGVK,
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "empty operations matches any operation for backward compatibility",
			applyTo: []MutationApplyTo{
				{
					ApplyTo: ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
				},
			},
			gvk:       podGVK,
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "empty operation string with explicit CREATE rejects mutation",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				},
			},
			gvk:       podGVK,
			operation: "",
			want:      false,
		},
		{
			name: "empty operation string with all supported operations matches mutation",
			applyTo: []MutationApplyTo{
				{
					ApplyTo:    ApplyTo{Groups: []string{""}, Kinds: []string{"Pod"}, Versions: []string{"v1"}},
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
				},
			},
			gvk:       podGVK,
			operation: "",
			want:      true,
		},
		{
			name:      "no applyTo entries matches nothing",
			applyTo:   []MutationApplyTo{},
			gvk:       podGVK,
			operation: admissionv1.Create,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppliesGVKAndOperation(tt.applyTo, tt.gvk, tt.operation)
			if got != tt.want {
				t.Errorf("AppliesGVKAndOperation() = %v, want %v", got, tt.want)
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
			name: "duplicate operation is rejected",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Create}},
			},
			wantErr: true,
			errMsg:  "duplicate operation \"CREATE\" in applyTo[0].operations",
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
		{
			name: "wildcard with CREATE is rejected",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll, admissionregistrationv1.Create}},
			},
			wantErr: true,
			errMsg:  "wildcard \"*\" in applyTo[0].operations must not be combined with other operations",
		},
		{
			name: "wildcard with multiple ops is rejected",
			applyTo: []MutationApplyTo{
				{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.OperationAll, admissionregistrationv1.Update}},
			},
			wantErr: true,
			errMsg:  "wildcard \"*\" in applyTo[0].operations must not be combined with other operations",
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
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateOperations() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}
