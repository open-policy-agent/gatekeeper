package match

import (
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AppliesTo checks if any item the given slice of ApplyTo applies to the given object.
func AppliesTo(applyTo []ApplyTo, gvk schema.GroupVersionKind) bool {
	for _, apply := range applyTo {
		if apply.Matches(gvk) {
			return true
		}
	}
	return false
}

// AppliesToMutation checks if any item the given slice of MutationApplyTo applies to the given object.
func AppliesToMutation(applyTo []MutationApplyTo, gvk schema.GroupVersionKind) bool {
	for _, apply := range applyTo {
		if apply.Matches(gvk) {
			return true
		}
	}
	return false
}

// AppliesOperationTo checks if any item in the given slice of MutationApplyTo allows the given operation.
func AppliesOperationTo(applyTo []MutationApplyTo, operation admissionv1.Operation) bool {
	for _, apply := range applyTo {
		if apply.MatchesOperation(operation) {
			return true
		}
	}
	return false
}

// ApplyTo determines what GVKs the resource applies to.
// Globs are not allowed.
// +kubebuilder:object:generate=true
type ApplyTo struct {
	Groups   []string `json:"groups,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

// MutationApplyTo extends ApplyTo with operation filtering for mutations.
// This type is used by mutation resources (Assign, AssignImage, ModifySet)
// to specify which GVKs and admission operations trigger the mutation.
// +kubebuilder:object:generate=true
type MutationApplyTo struct {
	ApplyTo `json:",inline"`
	// Operations specifies which admission operations (CREATE, UPDATE, DELETE, CONNECT) should trigger
	// this mutation. If empty, all operations are allowed for backward compatibility.
	// +kubebuilder:validation:items:Enum=CREATE;UPDATE;DELETE;CONNECT
	Operations []admissionv1.Operation `json:"operations,omitempty"`
}

// Flatten returns the set of GroupVersionKinds this ApplyTo matches.
// The GVKs are not guaranteed to be sorted or unique.
func (a *ApplyTo) Flatten() []schema.GroupVersionKind {
	var result []schema.GroupVersionKind
	for _, group := range a.Groups {
		for _, version := range a.Versions {
			for _, kind := range a.Kinds {
				gvk := schema.GroupVersionKind{
					Group:   group,
					Version: version,
					Kind:    kind,
				}
				result = append(result, gvk)
			}
		}
	}
	return result
}

// Matches returns true if the Object's Group, Version, and Kind are contained
// in the ApplyTo's match lists.
func (a *ApplyTo) Matches(gvk schema.GroupVersionKind) bool {
	if !contains(a.Groups, gvk.Group) {
		return false
	}
	if !contains(a.Versions, gvk.Version) {
		return false
	}
	if !contains(a.Kinds, gvk.Kind) {
		return false
	}

	return true
}

// Flatten returns the set of GroupVersionKinds this MutationApplyTo matches.
// The GVKs are not guaranteed to be sorted or unique.
func (a *MutationApplyTo) Flatten() []schema.GroupVersionKind {
	return a.ApplyTo.Flatten()
}

// Matches returns true if the Object's Group, Version, and Kind are contained
// in the MutationApplyTo's match lists.
func (a *MutationApplyTo) Matches(gvk schema.GroupVersionKind) bool {
	return a.ApplyTo.Matches(gvk)
}

// MatchesOperation returns true if the operation is contained in the MutationApplyTo's
// operations list. If no operations are specified, all operations are allowed
// for backward compatibility (consistent with how empty groups/versions/kinds
// work in ApplyTo - empty means match all).
// If operation is empty (e.g., in audit/expansion contexts), returns true
// to maintain backward compatibility with non-admission flows.
func (a *MutationApplyTo) MatchesOperation(operation admissionv1.Operation) bool {
	// If operation is empty (audit, expansion, or gator contexts), allow the mutation
	// These contexts don't have admission operations and should not be filtered
	if operation == "" {
		return true
	}

	// If no operations specified, allow all operations for backward compatibility
	// This is consistent with how empty groups/versions/kinds work (empty = match all)
	if len(a.Operations) == 0 {
		return true
	}

	// Check if the operation is explicitly allowed by the user
	return containsOperation(a.Operations, operation)
}

func containsOperation(list []admissionv1.Operation, s admissionv1.Operation) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
