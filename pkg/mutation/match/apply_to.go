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
	// Operations specifies which admission operations (CREATE, UPDATE, *) should trigger
	// this mutation. If empty, all operations supported by the mutation webhook are allowed for backward compatibility.
	// The Gatekeeper mutation webhook currently only processes CREATE and UPDATE operations, so those are the only
	// concrete values accepted. DELETE and CONNECT are rejected until the mutation webhook supports them.
	// "*" means all operations the mutation webhook currently supports and will broaden automatically if support
	// for additional operations is added in a future release.
	// +kubebuilder:validation:items:Enum=CREATE;UPDATE;*
	Operations []admissionregistrationv1.OperationType `json:"operations,omitempty"`
}

// supportedMutationOperations is the set of admission operations the mutation webhook currently executes.
// Adding an entry here (and registering the webhook for it) will broaden the effective scope of omitted-operations
// and "*" mutators on upgrade, so such a change must be deliberate and release-noted.
var supportedMutationOperations = []admissionv1.Operation{
	admissionv1.Create,
	admissionv1.Update,
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

// IsSupportedMutationOperation returns true if Gatekeeper currently executes mutation for the operation.
func IsSupportedMutationOperation(operation admissionv1.Operation) bool {
	return slices.Contains(supportedMutationOperations, operation)
}

// MatchesOperation returns true if the operation is supported by the mutation webhook and is contained in the
// MutationApplyTo's operations list. If no operations are specified, all supported mutation operations are allowed for
// backward compatibility. If operation is empty (e.g., in audit/expansion contexts), only mutators that apply to every
// supported mutation operation are allowed.
func (a *MutationApplyTo) MatchesOperation(operation admissionv1.Operation) bool {
	if operation == "" {
		return a.matchesAllSupportedMutationOperations()
	}

	if !IsSupportedMutationOperation(operation) {
		return false
	}

	if len(a.Operations) == 0 || slices.Contains(a.Operations, admissionregistrationv1.OperationAll) {
		return true
	}

	return slices.Contains(a.Operations, admissionregistrationv1.OperationType(operation))
}

func (a *MutationApplyTo) matchesAllSupportedMutationOperations() bool {
	if len(a.Operations) == 0 || slices.Contains(a.Operations, admissionregistrationv1.OperationAll) {
		return true
	}

	for _, operation := range supportedMutationOperations {
		if !slices.Contains(a.Operations, admissionregistrationv1.OperationType(operation)) {
			return false
		}
	}
	return true
}

// EffectiveOperations returns the admission operations this MutationApplyTo applies to for schema conflict detection.
// The mutation webhook currently only executes CREATE and UPDATE requests, so DELETE and CONNECT must not create
// schema-conflict bindings that can disable mutators for operations that actually run.
func (a *MutationApplyTo) EffectiveOperations() []admissionv1.Operation {
	if len(a.Operations) == 0 || slices.Contains(a.Operations, admissionregistrationv1.OperationAll) {
		return slices.Clone(supportedMutationOperations)
	}

	operations := make([]admissionv1.Operation, 0, len(supportedMutationOperations))
	for _, operation := range supportedMutationOperations {
		if slices.Contains(a.Operations, admissionregistrationv1.OperationType(operation)) {
			operations = append(operations, operation)
		}
	}
	return operations
}

// validOperations defines the set of operations accepted in a mutation applyTo. Only the operations the mutation
// webhook currently executes (CREATE, UPDATE) plus the "*" wildcard are accepted. DELETE and CONNECT are intentionally
// rejected until the mutation webhook supports them; accepting them now would permit valid-but-inert mutators and
// commit the API to silently activating them on a future upgrade.
var validOperations = sets.New[admissionregistrationv1.OperationType](
	admissionregistrationv1.Create,
	admissionregistrationv1.Update,
	admissionregistrationv1.OperationAll,
)

// ValidateOperations validates that all operations in the MutationApplyTo
// are operations the mutation webhook accepts (CREATE, UPDATE, *).
// It collates all errors and returns them together, following the Kubernetes
// validation pattern.
func ValidateOperations(applyTo []MutationApplyTo) error {
	var errs []string
	for i, apply := range applyTo {
		hasWildcard := false
		seen := sets.New[admissionregistrationv1.OperationType]()
		for _, op := range apply.Operations {
			if seen.Has(op) {
				errs = append(errs, fmt.Sprintf("duplicate operation %q in applyTo[%d].operations", op, i))
			}
			seen.Insert(op)
			if !validOperations.Has(op) {
				errs = append(errs, fmt.Sprintf("invalid operation %q in applyTo[%d].operations: must be one of CREATE, UPDATE, *", op, i))
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
