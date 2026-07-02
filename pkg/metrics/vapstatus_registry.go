package metrics

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

// VAPStatusRegistry tracks VAP/VAPB resources by their status for accurate counting.
// This is a thread-safe registry that can be used by both constraint and
// constrainttemplate controllers to track VAP-related resource statuses.
type VAPStatusRegistry struct {
	mu    sync.RWMutex
	cache map[types.NamespacedName]VAPStatus
}

// NewVAPStatusRegistry creates a new VAPStatusRegistry instance.
func NewVAPStatusRegistry() *VAPStatusRegistry {
	return &VAPStatusRegistry{cache: make(map[types.NamespacedName]VAPStatus)}
}

// Add adds or updates a resource's status in the registry.
// If the resource already exists with the same status, this is a no-op.
func (r *VAPStatusRegistry) Add(key types.NamespacedName, status VAPStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.cache[key]
	if ok && existing == status {
		return
	}
	r.cache[key] = status
}

// Remove removes a resource from the registry.
func (r *VAPStatusRegistry) Remove(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, key)
}

// ComputeTotals returns the count of resources for each status.
func (r *VAPStatusRegistry) ComputeTotals() map[VAPStatus]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totals := make(map[VAPStatus]int64)
	for _, status := range r.cache {
		totals[status]++
	}
	return totals
}
