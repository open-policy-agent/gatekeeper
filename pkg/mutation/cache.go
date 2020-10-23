package mutation

import (
	"fmt"
	"sort"
	"sync"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// Cache represent a mutation cache containting all the mutation resources,
// sorted with the following criteria:
// - group
// - kind
// - namespace
// - name
type Cache struct {
	mutators []Mutator
	sync.RWMutex
	scheme *runtime.Scheme
}

// NewCache initializes an empty cache
func NewCache(scheme *runtime.Scheme) *Cache {
	return &Cache{
		mutators: make([]Mutator, 0),
		scheme:   scheme,
	}
}

// Insert inserts the mutation resource into the cache
func (c *Cache) Insert(mutator Mutator) error {
	c.Lock()
	defer c.Unlock()
	var lastErr error
	i := sort.Search(len(c.mutators), func(i int) bool {
		res, err := c.greater(c.mutators[i], mutator)
		if err != nil {
			lastErr = err
		}
		return res
	})
	if lastErr != nil {
		return errors.Wrap(lastErr, "Error while inserting in cache")
	}

	if i == len(c.mutators) { // Adding to the bottom of the list
		c.mutators = append(c.mutators, mutator)
		return nil
	}
	updating, err := c.equal(c.mutators[i], mutator)
	if err != nil {
		return errors.Wrap(err, "Error while checking for equality in cache")
	}

	if updating {
		c.mutators[i] = mutator
		return nil
	}

	c.mutators = append(c.mutators, nil)
	copy(c.mutators[i+1:], c.mutators[i:])
	c.mutators[i] = mutator
	return nil
}

// Remove removes the mutation object from the cache
func (c *Cache) Remove(mutator Mutator) error {
	c.Lock()
	defer c.Unlock()
	var lastErr error
	i := sort.Search(len(c.mutators), func(i int) bool {
		res, err := c.equal(c.mutators[i], mutator)
		if err != nil {
			lastErr = err
		}
		if res {
			return true
		}
		res, err = c.greater(c.mutators[i], mutator)
		if err != nil {
			lastErr = err
		}
		return res
	})
	if lastErr != nil {
		return errors.Wrap(lastErr, "Error while removing from cache")
	}

	found, err := c.equal(c.mutators[i], mutator)
	if err != nil {
		return errors.Wrap(err, "Error while checking for equality in cache")
	}

	if !found {
		return nil
	}
	copy(c.mutators[i:], c.mutators[i+1:])
	c.mutators[len(c.mutators)-1] = nil
	c.mutators = c.mutators[:len(c.mutators)-1]
	return nil
}

// Iterate calls the given function with each element of the cache.
// It's meant to pass a copy of the mutator that should not modified.
// The order is the order in which the mutations are supposed to be
// applied.
func (c *Cache) Iterate(iterator func(mutator Mutator) error) error {
	c.RLock()
	defer c.RUnlock()
	for _, mutator := range c.mutators {
		err := iterator(mutator)
		if err != nil {
			return errors.Wrapf(err, "Iteration failed")
		}
	}
	return nil
}

func (c *Cache) greater(mutator1, mutator2 Mutator) (bool, error) {
	obj1 := mutator1.Obj()
	obj2 := mutator2.Obj()
	meta1, err := meta.Accessor(obj1)
	if err != nil {
		return false, fmt.Errorf("Accessor failed for %s", obj1.GetObjectKind().GroupVersionKind().Kind)
	}
	meta2, err := meta.Accessor(obj2)
	if err != nil {
		return false, fmt.Errorf("Accessor failed for %s", obj2.GetObjectKind().GroupVersionKind().Kind)
	}

	gvks1, _, err := c.scheme.ObjectKinds(obj1)
	if err != nil {
		return false, errors.Wrapf(err, "Failed finding group and kind for the first item")
	}
	gvks2, _, err := c.scheme.ObjectKinds(obj2)

	if err != nil {
		return false, errors.Wrapf(err, "Failed finding group and kind for the second item")
	}

	if gvks1[0].Group > gvks2[0].Group {
		return true, nil
	}
	if gvks1[0].Group < gvks2[0].Group {
		return false, nil
	}
	if gvks1[0].Kind > gvks2[0].Kind {
		return true, nil
	}
	if gvks1[0].Kind < gvks2[0].Kind {
		return false, nil
	}
	if meta1.GetNamespace() > meta2.GetNamespace() {
		return true, nil
	}
	if meta1.GetNamespace() < meta2.GetNamespace() {
		return false, nil
	}
	if meta1.GetName() > meta2.GetName() {
		return true, nil
	}
	if meta1.GetName() < meta2.GetName() {
		return false, nil
	}
	return false, nil
}

func (c *Cache) equal(mutator1, mutator2 Mutator) (bool, error) {
	obj1 := mutator1.Obj()
	obj2 := mutator2.Obj()

	meta1, err := meta.Accessor(obj1)
	if err != nil {
		return false, fmt.Errorf("Accessor failed for %s", obj1.GetObjectKind().GroupVersionKind().Kind)
	}
	meta2, err := meta.Accessor(obj2)
	if err != nil {
		return false, fmt.Errorf("Accessor failed for %s", obj2.GetObjectKind().GroupVersionKind().Kind)
	}
	gvks1, _, err := c.scheme.ObjectKinds(obj2)
	if err != nil {
		return false, errors.Wrapf(err, "Failed finding group and kind for the first item")
	}
	gvks2, _, err := c.scheme.ObjectKinds(obj2)
	if err != nil {
		return false, errors.Wrapf(err, "Failed finding group and kind for the first item")
	}
	if gvks1[0].Group == gvks2[0].Group &&
		gvks1[0].Kind == gvks2[0].Kind &&
		meta1.GetNamespace() == meta2.GetNamespace() &&
		meta1.GetName() == meta2.GetName() {
		return true, nil
	}
	return false, nil
}
