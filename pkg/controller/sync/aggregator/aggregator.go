package aggregator

import (
	"fmt"
	gosync "sync"

	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type KindName struct {
	Kind string
	Name string
}

func NewGVKAggregator(allowedKeyKinds []string) *GVKAgreggator {
	return &GVKAgreggator{
		store:        make(map[KindName]map[schema.GroupVersionKind]struct{}),
		reverseStore: make(map[schema.GroupVersionKind]map[KindName]struct{}),

		allowedKeyKinds: allowedKeyKinds,
	}
}

// todo comments.
type GVKAgreggator struct {
	mu gosync.RWMutex

	store        map[KindName]map[schema.GroupVersionKind]struct{}
	reverseStore map[schema.GroupVersionKind]map[KindName]struct{}

	allowedKeyKinds []string
}

func (b *GVKAgreggator) validateKind(k KindName) error {
	if len(b.allowedKeyKinds) == 0 {
		return nil
	}

	for _, kk := range b.allowedKeyKinds {
		if k.Kind == kk {
			// we found a match so allow the kind
			return nil
		}
	}

	return fmt.Errorf("unsupported key kind for key (%s, %s); supported values are: %s", k.Kind, k.Name, b.allowedKeyKinds)
}

func (b *GVKAgreggator) ListGVKs() []schema.GroupVersionKind {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return maps.Keys(b.reverseStore)
}

func (b *GVKAgreggator) Remove(k KindName) error {
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

func (b *GVKAgreggator) UpsertWithValidation(k KindName, gvks []schema.GroupVersionKind) error {
	if err := b.validateKind(k); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.store[k]; !ok {
		b.store[k] = make(map[schema.GroupVersionKind]struct{})
	}

	for _, gvk := range gvks {
		b.store[k][gvk] = struct{}{}
		if _, found := b.reverseStore[gvk]; !found {
			b.reverseStore[gvk] = make(map[KindName]struct{})
		}
		b.reverseStore[gvk][k] = struct{}{}
	}

	return nil
}
