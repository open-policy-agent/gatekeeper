package aggregator

import (
	"fmt"
	gosync "sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Key defines a type, identifier tuple to store
// in the GVKAggregator.
type Key struct {
	// Source specifies the type of the source object.
	Source string
	// ID specifies the name instance of the source object.
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
func (b *GVKAgreggator) Remove(k Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	gvks, found := b.store[k]
	if !found {
		return nil
	}

	if err := b.pruneReverseStore(gvks, k); err != nil {
		return err
	}

	delete(b.store, k)
	return nil
}

// Upsert stores an association between Key k and the list of GVKs
// and also the reverse associatoin between each GVK passed in and Key k.
// Any old associations are dropped, unless they are included in the new list of
// GVKs.
// It errors out if there is an internal issue with remove the reverse Key links
// for any GVKs that are being dropped as part of this Upsert call.
func (b *GVKAgreggator) Upsert(k Key, gvks []schema.GroupVersionKind) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	oldGVKs, found := b.store[k]
	if found {
		// gvksToRemove contains old GKVs that are not included in the new gvks list
		gvksToRemove := unreferencedOldGVKsToPrune(gvks, oldGVKs)
		if err := b.pruneReverseStore(gvksToRemove, k); err != nil {
			return fmt.Errorf("failed to prune entries on upsert: %w", err)
		}
	}

	// protect against empty inputs
	gvksSet := makeSet(gvks)
	if len(gvksSet) == 0 {
		return nil
	}

	b.store[k] = gvksSet
	// add reverse links
	for gvk := range gvksSet {
		if _, found := b.reverseStore[gvk]; !found {
			b.reverseStore[gvk] = make(map[Key]struct{})
		}
		b.reverseStore[gvk][k] = struct{}{}
	}

	return nil
}

// List returnes the gvk set for a given Key.
func (b *GVKAgreggator) List(k Key) map[schema.GroupVersionKind]struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	v := b.store[k]
	cpy := make(map[schema.GroupVersionKind]struct{}, len(v))
	for key, value := range v {
		cpy[key] = value
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

func (b *GVKAgreggator) pruneReverseStore(gvks map[schema.GroupVersionKind]struct{}, k Key) error {
	for gvk := range gvks {
		keySet, found := b.reverseStore[gvk]
		if !found || len(keySet) == 0 {
			// this should not happen if we keep the two maps well defined
			// but let's be defensive nonetheless.
			return fmt.Errorf("internal aggregator error: gvks stores are corrupted for key: %s", k)
		}

		delete(keySet, k)

		// remove GVK from reverseStore if it's not referenced by any Key anymore.
		if len(keySet) == 0 {
			delete(b.reverseStore, gvk)
		} else {
			b.reverseStore[gvk] = keySet
		}
	}

	return nil
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
