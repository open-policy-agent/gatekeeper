package aggregator

import (
	gosync "sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Key defines a type, identifier tuple to store
// in the GVKAggregator.
type Key struct {
	// Source specifies the type of the source object.
	Source string
	// ID specifies the name of the instance of the source object.
	ID string
}

func NewGVKAggregator() *GVKAgreggator {
	return &GVKAgreggator{
		store:        make(map[Key]map[schema.GroupVersionKind]struct{}),
		reverseStore: make(map[schema.GroupVersionKind]map[Key]struct{}),
	}
}

// GVKAgreggator is an implementation of a bi directional map
// that stores associations between Key K and GVKs and reverse associations
// between GVK g and Keys.
type GVKAgreggator struct {
	mu gosync.RWMutex

	// store keeps track of associations between a Key type and a set of GVKs.
	store map[Key]map[schema.GroupVersionKind]struct{}
	// reverseStore keeps track of associations between a GVK and the set of Key types
	// that references the GVK in the store map above. It is useful to have reverseStore
	// in order for IsPresent() and ListGVKs() to run in optimal time.
	reverseStore map[schema.GroupVersionKind]map[Key]struct{}
}

// IsPresent returns true if the given gvk is present in the GVKAggregator.
func (b *GVKAgreggator) IsPresent(gvk schema.GroupVersionKind) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	_, found := b.reverseStore[gvk]
	return found
}

// Remove deletes any associations that Key k has in the GVKAggregator.
// For any GVK in the association k --> [GVKs], we also delete any associations
// between the GVK and the Key k stored in the reverse map.
func (b *GVKAgreggator) Remove(k Key) {
	b.mu.Lock()
	defer b.mu.Unlock()

	gvks, found := b.store[k]
	if !found {
		return
	}

	b.pruneReverseStore(gvks, k)

	delete(b.store, k)
}

// Upsert stores an association between Key k and the list of GVKs
// and also the reverse association between each GVK passed in and Key k.
// Any old associations are dropped, unless they are included in the new list of
// GVKs.
func (b *GVKAgreggator) Upsert(k Key, gvks []schema.GroupVersionKind) {
	b.mu.Lock()
	defer b.mu.Unlock()

	oldGVKs, found := b.store[k]
	if found {
		// gvksToRemove contains old GKVs that are not included in the new gvks list
		gvksToRemove := unreferencedOldGVKsToPrune(gvks, oldGVKs)
		b.pruneReverseStore(gvksToRemove, k)
	}

	// protect against empty inputs
	gvksSet := makeSet(gvks)
	if len(gvksSet) == 0 {
		return
	}

	b.store[k] = gvksSet
	// add reverse links
	for gvk := range gvksSet {
		if _, found := b.reverseStore[gvk]; !found {
			b.reverseStore[gvk] = make(map[Key]struct{})
		}
		b.reverseStore[gvk][k] = struct{}{}
	}
}

// List returnes the gvk set for a given Key.
func (b *GVKAgreggator) List(k Key) []schema.GroupVersionKind {
	b.mu.RLock()
	defer b.mu.RUnlock()

	v := b.store[k]
	cpy := []schema.GroupVersionKind{}
	for key := range v {
		cpy = append(cpy, key)
	}
	return cpy
}

// GVKs returns a list of all of the schema.GroupVersionKind that are aggregated.
func (b *GVKAgreggator) GVKs() []schema.GroupVersionKind {
	b.mu.RLock()
	defer b.mu.RUnlock()

	allGVKs := []schema.GroupVersionKind{}
	for gvk := range b.reverseStore {
		allGVKs = append(allGVKs, gvk)
	}
	return allGVKs
}

func (b *GVKAgreggator) pruneReverseStore(gvks map[schema.GroupVersionKind]struct{}, k Key) {
	for gvk := range gvks {
		keySet, found := b.reverseStore[gvk]
		if !found {
			// by definition, nothing to prune
			return
		}

		delete(keySet, k)

		// remove GVK from reverseStore if it's not referenced by any Key anymore.
		if len(keySet) == 0 {
			delete(b.reverseStore, gvk)
		} else {
			b.reverseStore[gvk] = keySet
		}
	}
}

func makeSet(gvks []schema.GroupVersionKind) map[schema.GroupVersionKind]struct{} {
	gvkSet := make(map[schema.GroupVersionKind]struct{})
	for _, gvk := range gvks {
		if !gvk.Empty() {
			gvkSet[gvk] = struct{}{}
		}
	}

	return gvkSet
}

func unreferencedOldGVKsToPrune(newGVKs []schema.GroupVersionKind, oldGVKs map[schema.GroupVersionKind]struct{}) map[schema.GroupVersionKind]struct{} {
	// deep copy oldGVKs
	oldGVKsCpy := make(map[schema.GroupVersionKind]struct{}, len(oldGVKs))
	for k, v := range oldGVKs {
		oldGVKsCpy[k] = v
	}

	// intersection: exclude the oldGVKs that are present in the new GVKs as well.
	for _, newGVK := range newGVKs {
		// don't prune what is being already added
		delete(oldGVKsCpy, newGVK)
	}

	return oldGVKsCpy
}
