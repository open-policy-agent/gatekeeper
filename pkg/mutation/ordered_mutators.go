package mutation

import (
	"sort"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type orderedMutators struct {
	mutators []types.Mutator
}

func (ms *orderedMutators) insert(toAdd types.Mutator) {
	i, found := ms.find(toAdd.ID())
	if i == len(ms.mutators) {
		// Add to the end of the list.
		ms.mutators = append(ms.mutators, toAdd)
		return
	}

	if found {
		ms.mutators[i] = toAdd
		return
	}

	ms.mutators = append(ms.mutators, nil)
	copy(ms.mutators[i+1:], ms.mutators[i:])
	ms.mutators[i] = toAdd
}

// remove removes the Mutator with id. Returns true if the Mutator was removed,
// or false if the Mutator was not found.
func (ms *orderedMutators) remove(id types.ID) bool {
	i, found := ms.find(id)
	// The map is expected to be in sync with the list, so if we don't find it
	// we return an error.
	if !found {
		return false
	}

	copy(ms.mutators[i:], ms.mutators[i+1:])
	ms.mutators[len(ms.mutators)-1] = nil
	ms.mutators = ms.mutators[:len(ms.mutators)-1]

	return true
}

func (ms *orderedMutators) find(id types.ID) (int, bool) {
	idx := sort.Search(len(ms.mutators), func(i int) bool {
		return greaterOrEqual(ms.mutators[i].ID(), id)
	})

	if idx == len(ms.mutators) {
		return idx, false
	}

	return idx, ms.mutators[idx].ID() == id
}

func greaterOrEqual(id1, id2 types.ID) bool {
	if id1.Group != id2.Group {
		return id1.Group > id2.Group
	}
	if id1.Kind != id2.Kind {
		return id1.Kind > id2.Kind
	}
	if id1.Namespace != id2.Namespace {
		return id1.Namespace > id2.Namespace
	}
	return id1.Name >= id2.Name
}
