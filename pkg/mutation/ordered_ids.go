package mutation

import (
	"sort"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type orderedIDs struct {
	ids []types.ID
}

func (o *orderedIDs) insert(id types.ID) {
	i, found := o.find(id)
	if i == len(o.ids) {
		// Add to the end of the list.
		o.ids = append(o.ids, id)
		return
	}

	if found {
		o.ids[i] = id
		return
	}

	o.ids = append(o.ids, types.ID{})
	copy(o.ids[i+1:], o.ids[i:])
	o.ids[i] = id
}

// remove removes the Mutator with id. Returns true if the Mutator was removed,
// or false if the Mutator was not found.
func (o *orderedIDs) remove(id types.ID) bool {
	i, found := o.find(id)
	// The map is expected to be in sync with the list, so if we don't find it
	// we return an error.
	if !found {
		return false
	}

	copy(o.ids[i:], o.ids[i+1:])
	o.ids[len(o.ids)-1] = types.ID{}
	o.ids = o.ids[:len(o.ids)-1]

	return true
}

func (o *orderedIDs) find(id types.ID) (int, bool) {
	idx := sort.Search(len(o.ids), func(i int) bool {
		return greaterOrEqual(o.ids[i], id)
	})

	if idx == len(o.ids) {
		return idx, false
	}

	return idx, o.ids[idx] == id
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
