package match

import (
	"fmt"
	"slices"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
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

// AppliesGVKAndOperation checks if at least one entry in the given slice of
// MutationApplyTo matches BOTH the given GVK and the given operation. This
// prevents false positives where one entry matches GVK and a different entry
// matches the operation.
func AppliesGVKAndOperation(applyTo []MutationApplyTo, gvk schema.GroupVersionKind, operation admissionv1.Operation) bool {
	for _, apply := range applyTo {
		if apply.Matches(gvk) && apply.MatchesOperation(operation) {
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
	// Operations specifies which admission operations (CREATE, UPDATE, DELETE, CONNECT, *) should trigger
	// this mutation. If empty, all operations are allowed for backward compatibility.
	// +kubebuilder:validation:items:Enum=CREATE;UPDATE;DELETE;CONNECT;*
	Operations []admissionregistrationv1.OperationType `json:"operations,omitempty"`
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
// for backward compatibility.
// If operation is empty (e.g., in audit/expansion contexts), returns true
// to maintain backward compatibility with non-admission flows.
func (a *MutationApplyTo) MatchesOperation(operation admissionv1.Operation) bool {
	// If operation is empty (audit, expansion, or gator contexts), allow the mutation
	// These contexts don't have admission operations and should not be filtered
	if operation == "" {
		return true
	}

	// If no operations specified, allow all operations for backward compatibility.
	// Note: this differs from groups/versions/kinds where empty means no match.
	// Empty operations means "no operation filtering" to preserve existing behavior.
	if len(a.Operations) == 0 {
		return true
	}

	if slices.Contains(a.Operations, admissionregistrationv1.OperationAll) {
		return true
	}

	// Check if the operation is explicitly allowed by the user
	// Convert admissionv1.Operation to admissionregistrationv1.OperationType for comparison
	opType := admissionregistrationv1.OperationType(operation)
	return slices.Contains(a.Operations, opType)
}

// validOperations defines the set of valid admission operations.
var validOperations = sets.New[admissionregistrationv1.OperationType](
	admissionregistrationv1.Create,
	admissionregistrationv1.Update,
	admissionregistrationv1.Delete,
	admissionregistrationv1.Connect,
	admissionregistrationv1.OperationAll,
)

// ValidateOperations validates that all operations in the MutationApplyTo
// are valid Kubernetes admission operations (CREATE, UPDATE, DELETE, CONNECT, *).
// It collates all errors and returns them together, following the Kubernetes
// validation pattern.
func ValidateOperations(applyTo []MutationApplyTo) error {
	var errs []string
	for i, apply := range applyTo {
		hasWildcard := false
		for _, op := range apply.Operations {
			if !validOperations.Has(op) {
				errs = append(errs, fmt.Sprintf("invalid operation %q in applyTo[%d].operations: must be one of CREATE, UPDATE, DELETE, CONNECT, *", op, i))
			}
			if op == admissionregistrationv1.OperationAll {
				hasWildcard = true
			}
		}
		if hasWildcard && len(apply.Operations) > 1 {
			errs = append(errs, fmt.Sprintf("wildcard \"*\" in applyTo[%d].operations must not be combined with other operations", i))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
