package mutators

import (
	"sync"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type mutatorStatus struct {
	ingestion MutatorIngestionStatus
	conflict  bool
}

type Cache struct {
	cache map[types.ID]mutatorStatus
	mux   sync.RWMutex
}

func NewMutationCache() *Cache {
	return &Cache{
		cache: make(map[types.ID]mutatorStatus),
	}
}

func (c *Cache) Upsert(mID types.ID, ingestionStatus MutatorIngestionStatus, conflict bool) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.cache[mID] = mutatorStatus{
		ingestion: ingestionStatus,
		conflict:  conflict,
	}
}

func (c *Cache) Remove(mID types.ID) {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.cache, mID)
}

// TallyStatus calculates the number of mutators in each of the available
// MutatorIngestionStatus states and returns a map of those states and the
// count for each.
func (c *Cache) TallyStatus() map[MutatorIngestionStatus]int {
	c.mux.RLock()
	defer c.mux.RUnlock()

	statusTally := map[MutatorIngestionStatus]int{
		MutatorStatusActive: 0,
		MutatorStatusError:  0,
	}
	for _, status := range c.cache {
		statusTally[status.ingestion]++
	}
	return statusTally
}

// TallyConflict calculates and returns the number of mutators that are
// currently in a conflict state as maintained in the Cache.
func (c *Cache) TallyConflict() int {
	c.mux.RLock()
	defer c.mux.RUnlock()

	conflicts := 0
	for _, status := range c.cache {
		if status.conflict {
			conflicts++
		}
	}

	return conflicts
}
