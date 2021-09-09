package mutation

import (
	"fmt"
	"sort"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type orderedMutators struct {
	mutators []types.Mutator
}

func (ms *orderedMutators) insert(toAdd types.Mutator) error {
	i := ms.find(toAdd.ID())
	if i == len(ms.mutators) {
		// Add to the end of the list.
		ms.mutators = append(ms.mutators, toAdd)
		return nil
	}

	found := ms.mutators[i].ID() == toAdd.ID()
	if found {
		ms.mutators[i] = toAdd
		return nil
	}

	ms.mutators = append(ms.mutators, nil)
	copy(ms.mutators[i+1:], ms.mutators[i:])
	ms.mutators[i] = toAdd

	return nil
}

func (ms *orderedMutators) remove(id types.ID) error {
	i := ms.find(id)
	if i >= len(ms.mutators) {
		return fmt.Errorf("failed to find mutator with ID %v on sorted list", id)
	}

	// The map is expected to be in sync with the list, so if we don't find it
	// we return an error.
	found := ms.mutators[i].ID() == id
	if !found {
		return fmt.Errorf("failed to find mutator with ID %v on sorted list", id)
	}

	copy(ms.mutators[i:], ms.mutators[i+1:])
	ms.mutators[len(ms.mutators)-1] = nil
	ms.mutators = ms.mutators[:len(ms.mutators)-1]

	return nil
}

func (ms *orderedMutators) find(id types.ID) int {
	return sort.Search(len(ms.mutators), func(i int) bool {
		return greaterOrEqual(ms.mutators[i].ID(), id)
	})
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
