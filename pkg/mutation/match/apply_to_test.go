package match

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestApplyTo_MatchesOperation(t *testing.T) {
	tests := []struct {
		name      string
		applyTo   ApplyTo
		operation string
		want      bool
	}{
		{
			name:      "empty operations - defaults to CREATE",
			applyTo:   ApplyTo{},
			operation: "CREATE",
			want:      true,
		},
		{
			name:      "empty operations - defaults to UPDATE",
			applyTo:   ApplyTo{},
			operation: "UPDATE", 
			want:      true,
		},
		{
			name:      "empty operations - rejects DELETE (backward compatibility)",
			applyTo:   ApplyTo{},
			operation: "DELETE",
			want:      false,
		},
		{
			name: "explicit CREATE only",
			applyTo: ApplyTo{
				Operations: []string{"CREATE"},
			},
			operation: "CREATE",
			want:      true,
		},
		{
			name: "explicit CREATE only - rejects UPDATE",
			applyTo: ApplyTo{
				Operations: []string{"CREATE"},
			},
			operation: "UPDATE",
			want:      false,
		},
		{
			name: "explicit UPDATE only",
			applyTo: ApplyTo{
				Operations: []string{"UPDATE"},
			},
			operation: "UPDATE",
			want:      true,
		},
		{
			name: "explicit UPDATE only - rejects CREATE",
			applyTo: ApplyTo{
				Operations: []string{"UPDATE"},
			},
			operation: "CREATE",
			want:      false,
		},
		{
			name: "multiple operations - CREATE and UPDATE",
			applyTo: ApplyTo{
				Operations: []string{"CREATE", "UPDATE"},
			},
			operation: "CREATE",
			want:      true,
		},
		{
			name: "multiple operations - CREATE and UPDATE with UPDATE",
			applyTo: ApplyTo{
				Operations: []string{"CREATE", "UPDATE"},
			},
			operation: "UPDATE",
			want:      true,
		},
		{
			name: "multiple operations - rejects DELETE",
			applyTo: ApplyTo{
				Operations: []string{"CREATE", "UPDATE"},
			},
			operation: "DELETE",
			want:      false,
		},
		{
			name: "DELETE operation allowed when explicitly specified",
			applyTo: ApplyTo{
				Operations: []string{"DELETE"},
			},
			operation: "DELETE",
			want:      true,
		},
		{
			name: "multiple operations including DELETE",
			applyTo: ApplyTo{
				Operations: []string{"CREATE", "UPDATE", "DELETE"},
			},
			operation: "DELETE",
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
		applyTo   []ApplyTo
		operation string
		want      bool
	}{
		{
			name:      "empty slice",
			applyTo:   []ApplyTo{},
			operation: "CREATE",
			want:      false,
		},
		{
			name: "single ApplyTo - matches CREATE",
			applyTo: []ApplyTo{
				{Operations: []string{"CREATE"}},
			},
			operation: "CREATE",
			want:      true,
		},
		{
			name: "single ApplyTo - no match",
			applyTo: []ApplyTo{
				{Operations: []string{"CREATE"}},
			},
			operation: "UPDATE",
			want:      false,
		},
		{
			name: "multiple ApplyTo - first matches",
			applyTo: []ApplyTo{
				{Operations: []string{"CREATE"}},
				{Operations: []string{"UPDATE"}},
			},
			operation: "CREATE",
			want:      true,
		},
		{
			name: "multiple ApplyTo - second matches",
			applyTo: []ApplyTo{
				{Operations: []string{"CREATE"}},
				{Operations: []string{"UPDATE"}},
			},
			operation: "UPDATE",
			want:      true,
		},
		{
			name: "multiple ApplyTo - no match",
			applyTo: []ApplyTo{
				{Operations: []string{"CREATE"}},
				{Operations: []string{"UPDATE"}},
			},
			operation: "DELETE",
			want:      false,
		},
		{
			name: "mixed with default behavior",
			applyTo: []ApplyTo{
				{Operations: []string{"DELETE"}},  // Only DELETE (explicitly allowed)
				{},                                // Defaults to CREATE, UPDATE
			},
			operation: "CREATE",
			want:      true, // Second ApplyTo allows CREATE
		},
		{
			name: "mixed with default behavior - UPDATE",
			applyTo: []ApplyTo{
				{Operations: []string{"DELETE"}},  // Only DELETE (explicitly allowed)
				{},                                // Defaults to CREATE, UPDATE
			},
			operation: "UPDATE",
			want:      true, // Second ApplyTo allows UPDATE
		},
		{
			name: "mixed with default behavior - DELETE allowed when specified",
			applyTo: []ApplyTo{
				{Operations: []string{"DELETE"}},  // Only DELETE (explicitly allowed)
				{},                                // Defaults to CREATE, UPDATE
			},
			operation: "DELETE",
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
		applyTo ApplyTo
		gvk     schema.GroupVersionKind
		want    bool
	}{
		{
			name: "basic GVK match with operations field present",
			applyTo: ApplyTo{
				Groups:     []string{"apps"},
				Kinds:      []string{"Deployment"},
				Versions:   []string{"v1"},
				Operations: []string{"CREATE"}, // This should not affect GVK matching
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
			applyTo: ApplyTo{
				Groups:     []string{"apps"},
				Kinds:      []string{"Deployment"},
				Versions:   []string{"v1"},
				Operations: []string{"CREATE"}, // This should not affect GVK matching
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
