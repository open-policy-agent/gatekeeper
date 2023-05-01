package aggregator

import (
	"fmt"
	gosync "sync"

	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	syncset    = "TODOa"
	configsync = "TODOb"
)

type GVKAgreggator interface {
	UpsertWithValidation(k Key, gvks []schema.GroupVersionKind) (bool, error)
	Remove(k Key) error
	GetGVKs(k Key) ([]schema.GroupVersionKind, bool)
	ListKeys() []Key
	ListGVKs() []schema.GroupVersionKind
}

type Key struct {
	Kind string
	Name string
}

func (k *Key) ValidateKind() error {
	if k.Kind != syncset && k.Kind != configsync {
		return fmt.Errorf("unsupported key kind for (%s, %s); supported values are: %s", k.Kind, k.Name, "a, b")
	}

	return nil
}

func NewGVKAggregator() GVKAgreggator {
	return &bidiGVKAggregator{
		store:        make(map[Key]map[schema.GroupVersionKind]struct{}),
		reverseStore: make(map[schema.GroupVersionKind]map[Key]struct{}),
	}
}

// todo comments.
type bidiGVKAggregator struct {
	mu gosync.RWMutex

	store        map[Key]map[schema.GroupVersionKind]struct{}
	reverseStore map[schema.GroupVersionKind]map[Key]struct{}
}

// GetGVKs implements GVKAgreggator.
func (b *bidiGVKAggregator) GetGVKs(k Key) ([]schema.GroupVersionKind, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	gvks, found := b.store[k]
	if !found {
		return nil, false
	}

	return maps.Keys(gvks), true
}

// ListGVKs implements GVKAgreggator.
func (b *bidiGVKAggregator) ListGVKs() []schema.GroupVersionKind {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return maps.Keys(b.reverseStore)
}

// ListKeys implements GVKAgreggator.
func (b *bidiGVKAggregator) ListKeys() []Key {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return maps.Keys(b.store)
}

// Remove implements GVKAgreggator.
func (b *bidiGVKAggregator) Remove(k Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	gvks, found := b.store[k]
	if !found {
		return nil
	}

	for gvk := range gvks {
		keySet, found := b.reverseStore[gvk]
		if !found {
			// this should not happen if we keep the two maps are well defined
			// but let's be defensive nonetheless

			return fmt.Errorf("internal aggregator error: gvks stores are corrupted for key: %s", k)
		}

		delete(keySet, k)

		b.reverseStore[gvk] = keySet
	}

	delete(b.store, k)
	return nil
}

// UpsertWithValidation implements GVKAgreggator.
func (b *bidiGVKAggregator) UpsertWithValidation(k Key, gvks []schema.GroupVersionKind) (bool, error) {
	if err := k.ValidateKind(); err != nil {
		return false, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.store[k]; !ok {
		b.store[k] = make(map[schema.GroupVersionKind]struct{})
	}

	for _, gvk := range gvks {
		b.store[k][gvk] = struct{}{}
		if _, found := b.reverseStore[gvk]; !found {
			b.reverseStore[gvk] = make(map[Key]struct{})
		}
		b.reverseStore[gvk][k] = struct{}{}
	}

	return true, nil
}

var _ GVKAgreggator = &bidiGVKAggregator{}
