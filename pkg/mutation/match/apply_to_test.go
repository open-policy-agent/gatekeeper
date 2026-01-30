package match

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
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
				Operations: []admissionv1.Operation{admissionv1.Create},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "explicit CREATE only - rejects UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Create},
			},
			operation: admissionv1.Update,
			want:      false,
		},
		{
			name: "explicit UPDATE only",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Update},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "explicit UPDATE only - rejects CREATE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Update},
			},
			operation: admissionv1.Create,
			want:      false,
		},
		{
			name: "multiple operations - CREATE and UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "multiple operations - CREATE and UPDATE with UPDATE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "multiple operations - rejects DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Create, admissionv1.Update},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "DELETE operation allowed when explicitly specified",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Delete},
			},
			operation: admissionv1.Delete,
			want:      true,
		},
		{
			name: "multiple operations including DELETE",
			applyTo: MutationApplyTo{
				Operations: []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete},
			},
			operation: admissionv1.Delete,
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

func TestAppliesOperationTo(t *testing.T) {
	tests := []struct {
		name      string
		applyTo   []MutationApplyTo
		operation admissionv1.Operation
		want      bool
	}{
		{
			name:      "empty slice",
			applyTo:   []MutationApplyTo{},
			operation: admissionv1.Create,
			want:      false,
		},
		{
			name: "single ApplyTo - matches CREATE",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Create}},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "single ApplyTo - no match",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Create}},
			},
			operation: admissionv1.Update,
			want:      false,
		},
		{
			name: "multiple ApplyTo - first matches",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Create}},
				{Operations: []admissionv1.Operation{admissionv1.Update}},
			},
			operation: admissionv1.Create,
			want:      true,
		},
		{
			name: "multiple ApplyTo - second matches",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Create}},
				{Operations: []admissionv1.Operation{admissionv1.Update}},
			},
			operation: admissionv1.Update,
			want:      true,
		},
		{
			name: "multiple ApplyTo - no match",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Create}},
				{Operations: []admissionv1.Operation{admissionv1.Update}},
			},
			operation: admissionv1.Delete,
			want:      false,
		},
		{
			name: "mixed with default behavior",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Delete}}, // Only DELETE (explicitly allowed)
				{}, // Defaults to CREATE, UPDATE
			},
			operation: admissionv1.Create,
			want:      true, // Second ApplyTo allows CREATE
		},
		{
			name: "mixed with default behavior - UPDATE",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Delete}}, // Only DELETE (explicitly allowed)
				{}, // Defaults to CREATE, UPDATE
			},
			operation: admissionv1.Update,
			want:      true, // Second ApplyTo allows UPDATE
		},
		{
			name: "mixed with default behavior - DELETE allowed when specified",
			applyTo: []MutationApplyTo{
				{Operations: []admissionv1.Operation{admissionv1.Delete}}, // Only DELETE (explicitly allowed)
				{}, // Defaults to CREATE, UPDATE
			},
			operation: admissionv1.Delete,
			want:      true, // First ApplyTo explicitly allows DELETE
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppliesOperationTo(tt.applyTo, tt.operation)
			if got != tt.want {
				t.Errorf("AppliesOperationTo() = %v, want %v", got, tt.want)
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
				Operations: []admissionv1.Operation{admissionv1.Create}, // This should not affect GVK matching
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
				Operations: []admissionv1.Operation{admissionv1.Create}, // This should not affect GVK matching
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
