package mutation

import (
	"sort"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type orderedMutators struct {
	ids []types.ID
}

func (ms *orderedMutators) insert(id types.ID) {
	i, found := ms.find(id)
	if i == len(ms.ids) {
		// Add to the end of the list.
		ms.ids = append(ms.ids, id)
		return
	}

	if found {
		ms.ids[i] = id
		return
	}

	ms.ids = append(ms.ids, types.ID{})
	copy(ms.ids[i+1:], ms.ids[i:])
	ms.ids[i] = id
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

	copy(ms.ids[i:], ms.ids[i+1:])
	ms.ids[len(ms.ids)-1] = types.ID{}
	ms.ids = ms.ids[:len(ms.ids)-1]

	return true
}

func (ms *orderedMutators) find(id types.ID) (int, bool) {
	idx := sort.Search(len(ms.ids), func(i int) bool {
		return greaterOrEqual(ms.ids[i], id)
	})

	if idx == len(ms.ids) {
		return idx, false
	}

	return idx, ms.ids[idx] == id
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
