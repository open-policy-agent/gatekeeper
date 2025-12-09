package match

import "k8s.io/apimachinery/pkg/runtime/schema"

// AppliesTo checks if any item the given slice of ApplyTo applies to the given object.
func AppliesTo(applyTo []ApplyTo, gvk schema.GroupVersionKind) bool {
	for _, apply := range applyTo {
		if apply.Matches(gvk) {
			return true
		}
	}
	return false
}

// AppliesOperationTo checks if any item in the given slice of ApplyTo allows the given operation.
func AppliesOperationTo(applyTo []ApplyTo, operation string) bool {
	for _, apply := range applyTo {
		if apply.MatchesOperation(operation) {
			return true
		}
	}
	return false
}

// ApplyTo determines what GVKs and operations the mutation should apply to.
// Globs are not allowed.
// +kubebuilder:object:generate=true
type ApplyTo struct {
	Groups     []string `json:"groups,omitempty"`
	Kinds      []string `json:"kinds,omitempty"`
	Versions   []string `json:"versions,omitempty"`
	// Operations specifies which admission operations (CREATE, UPDATE, DELETE) should trigger
	// this mutation. If empty, defaults to ["CREATE", "UPDATE"] for backward compatibility.
	Operations []string `json:"operations,omitempty"`
}

// Flatten returns the set of GroupVersionKinds this ApplyTo matches.
// The GVKs are not guaranteed to be sorted or unique.
func (a ApplyTo) Flatten() []schema.GroupVersionKind {
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
func (a ApplyTo) Matches(gvk schema.GroupVersionKind) bool {
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

// MatchesOperation returns true if the operation is contained in the ApplyTo's
// operations list. If no operations are specified, it defaults to allowing
// CREATE and UPDATE for backward compatibility. Users can explicitly specify
// DELETE operations if they have legitimate use cases.
func (a ApplyTo) MatchesOperation(operation string) bool {
	// If no operations specified, default to CREATE and UPDATE for backward compatibility
	if len(a.Operations) == 0 {
		return operation == "CREATE" || operation == "UPDATE"
	}
	
	// Check if the operation is explicitly allowed by the user
	return contains(a.Operations, operation)
}
