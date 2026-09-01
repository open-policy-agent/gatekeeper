package mutation

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	mutationschema "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	applyToWildcard    = "*"
	apiVersionGVKField = "apiVersion"
	kindGVKField       = "kind"
)

// candidateIndex tracks mutators by the schema bindings that can make them
// relevant for a mutation request. Mutators without schema bindings are kept in
// a separate ordered list because their Matches implementation remains the only
// source of applicability information.
type candidateIndex struct {
	byBinding   map[mutationschema.Binding]orderedIDs
	byGVK       map[runtimeschema.GroupVersionKind]orderedIDs
	unindexed   orderedIDs
	gvkChanging orderedIDs
}

func newCandidateIndex() candidateIndex {
	return candidateIndex{
		byBinding: make(map[mutationschema.Binding]orderedIDs),
		byGVK:     make(map[runtimeschema.GroupVersionKind]orderedIDs),
	}
}

func (idx *candidateIndex) add(mutator types.Mutator) {
	if mutator == nil {
		return
	}

	bindings, indexed := indexableBindings(mutator)
	id := mutator.ID()
	idx.gvkChanging.remove(id)
	if mutatorMayChangeGVK(mutator) {
		idx.gvkChanging.insert(id)
	}
	if !indexed {
		idx.unindexed.insert(id)
		return
	}

	idx.ensureMaps()
	for _, binding := range bindings {
		byBinding := idx.byBinding[binding]
		byBinding.insert(id)
		idx.byBinding[binding] = byBinding

		byGVK := idx.byGVK[binding.GVK]
		byGVK.insert(id)
		idx.byGVK[binding.GVK] = byGVK
	}
}

func (idx *candidateIndex) remove(mutator types.Mutator) {
	if mutator == nil {
		return
	}

	bindings, indexed := indexableBindings(mutator)
	id := mutator.ID()
	idx.gvkChanging.remove(id)
	if !indexed {
		idx.unindexed.remove(id)
		return
	}

	for _, binding := range bindings {
		if byBinding, ok := idx.byBinding[binding]; ok {
			byBinding.remove(id)
			if len(byBinding.ids) == 0 {
				delete(idx.byBinding, binding)
			} else {
				idx.byBinding[binding] = byBinding
			}
		}

		if byGVK, ok := idx.byGVK[binding.GVK]; ok {
			byGVK.remove(id)
			if len(byGVK.ids) == 0 {
				delete(idx.byGVK, binding.GVK)
			} else {
				idx.byGVK[binding.GVK] = byGVK
			}
		}
	}
}

func (idx *candidateIndex) candidates(binding mutationschema.Binding) []types.ID {
	candidates := idx.unindexed.ids
	for groupIndex := 0; ; groupIndex++ {
		group, ok := exactOrWildcardAt(binding.GVK.Group, groupIndex)
		if !ok {
			break
		}
		for versionIndex := 0; ; versionIndex++ {
			version, ok := exactOrWildcardAt(binding.GVK.Version, versionIndex)
			if !ok {
				break
			}
			for kindIndex := 0; ; kindIndex++ {
				kind, ok := exactOrWildcardAt(binding.GVK.Kind, kindIndex)
				if !ok {
					break
				}
				candidateBinding := mutationschema.Binding{
					GVK:       runtimeschema.GroupVersionKind{Group: group, Version: version, Kind: kind},
					Operation: binding.Operation,
				}
				candidates = mergeOrderedIDs(candidates, idx.byBinding[candidateBinding].ids)
			}
		}
	}
	return candidates
}

func (idx *candidateIndex) candidatesForGVK(gvk runtimeschema.GroupVersionKind) []types.ID {
	candidates := idx.unindexed.ids
	for groupIndex := 0; ; groupIndex++ {
		group, ok := exactOrWildcardAt(gvk.Group, groupIndex)
		if !ok {
			break
		}
		for versionIndex := 0; ; versionIndex++ {
			version, ok := exactOrWildcardAt(gvk.Version, versionIndex)
			if !ok {
				break
			}
			for kindIndex := 0; ; kindIndex++ {
				kind, ok := exactOrWildcardAt(gvk.Kind, kindIndex)
				if !ok {
					break
				}
				candidateGVK := runtimeschema.GroupVersionKind{Group: group, Version: version, Kind: kind}
				candidates = mergeOrderedIDs(candidates, idx.byGVK[candidateGVK].ids)
			}
		}
	}
	return candidates
}

func exactOrWildcardAt(value string, index int) (string, bool) {
	switch index {
	case 0:
		return value, true
	case 1:
		if value != applyToWildcard {
			return applyToWildcard, true
		}
	}
	return "", false
}

func (idx *candidateIndex) hasGVKChangingMutators() bool {
	return len(idx.gvkChanging.ids) > 0
}

func mutatorMayChangeGVK(mutator types.Mutator) bool {
	path := mutator.Path()
	if len(path.Nodes) == 0 {
		return false
	}
	var reference string
	switch node := path.Nodes[0].(type) {
	case parser.Object:
		reference = node.Reference
	case *parser.Object:
		reference = node.Reference
	default:
		return false
	}
	return reference == apiVersionGVKField || reference == kindGVKField
}

func (idx *candidateIndex) ensureMaps() {
	if idx.byBinding == nil {
		idx.byBinding = make(map[mutationschema.Binding]orderedIDs)
	}
	if idx.byGVK == nil {
		idx.byGVK = make(map[runtimeschema.GroupVersionKind]orderedIDs)
	}
}

func indexableBindings(mutator types.Mutator) ([]mutationschema.Binding, bool) {
	withSchema, ok := mutator.(mutationschema.MutatorWithSchema)
	if !ok {
		return nil, false
	}

	bindings := withSchema.SchemaBindings()
	if len(bindings) == 0 {
		return nil, false
	}
	return bindings, true
}

func mergeOrderedIDs(left, right []types.ID) []types.ID {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}

	merged := make([]types.ID, 0, len(left)+len(right))
	for i, j := 0, 0; i < len(left) || j < len(right); {
		var next types.ID
		switch {
		case i == len(left):
			next = right[j]
			j++
		case j == len(right):
			next = left[i]
			i++
		case left[i] == right[j]:
			next = left[i]
			i++
			j++
		case lessID(left[i], right[j]):
			next = left[i]
			i++
		default:
			next = right[j]
			j++
		}

		if len(merged) == 0 || merged[len(merged)-1] != next {
			merged = append(merged, next)
		}
	}

	return merged
}

func lessID(left, right types.ID) bool {
	return !greaterOrEqual(left, right)
}
