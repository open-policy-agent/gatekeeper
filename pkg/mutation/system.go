package mutation

import (
	"fmt"
	"sort"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// System keeps the list of mutations and
// provides an interface to apply mutations.
type System struct {
	schemaDB        SchemaDB
	orderedMutators []Mutator
	mutatorsMap     map[ID]Mutator
	sync.RWMutex
}

// NewSystem initializes an empty mutation system
func NewSystem() *System {
	return &System{
		orderedMutators: make([]Mutator, 0),
		mutatorsMap:     make(map[ID]Mutator),
	}
}

// Upsert updates or insert the given object, and returns
// an error in case of conflicts
func (s *System) Upsert(m Mutator) error {
	s.Lock()
	defer s.Unlock()

	current, ok := s.mutatorsMap[m.ID()]
	if ok && !m.HasDiff(current) {
		return nil
	}

	toAdd := m.DeepCopy()

	// Checking schema consistency only if the mutator has schema
	if withSchema, ok := toAdd.(MutatorWithSchema); ok {
		err := s.schemaDB.Upsert(withSchema)
		if err != nil {
			return errors.Wrapf(err, "Schema upsert failed")
		}
	}

	s.mutatorsMap[toAdd.ID()] = toAdd

	i := sort.Search(len(s.orderedMutators), func(i int) bool {
		return greaterOrEqual(s.orderedMutators[i], toAdd)
	})

	if i == len(s.orderedMutators) { // Adding to the bottom of the list
		s.orderedMutators = append(s.orderedMutators, toAdd)
		return nil
	}

	found := equal(s.orderedMutators[i], toAdd)
	if found {
		s.orderedMutators[i] = toAdd
		return nil
	}

	s.orderedMutators = append(s.orderedMutators, nil)
	copy(s.orderedMutators[i+1:], s.orderedMutators[i:])
	s.orderedMutators[i] = toAdd
	return nil
}

// Mutate applies the mutation in place to the given object
func (s *System) Mutate(obj *unstructured.Unstructured, ns *corev1.Namespace) error {
	s.RLock()
	defer s.RUnlock()

	maxIterations := len(s.orderedMutators) + 1

	for i := 0; i < maxIterations; i++ {
		old := obj.DeepCopy()
		for _, m := range s.orderedMutators {
			if m.Matches(obj, ns) {
				err := m.Mutate(obj)
				if err != nil {
					return errors.Wrapf(err, "Mutation failed for %s %s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind)
				}
			}
		}
		if cmp.Equal(old, obj) {
			return nil
		}
	}

	return fmt.Errorf("Mutation not converging for %s %s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind)
}

// Remove removes the mutator from the mutation system
func (s *System) Remove(m Mutator) error {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.mutatorsMap[m.ID()]; !ok {
		return nil
	}

	err := s.schemaDB.Remove(m.ID())
	// TODO: get back on this once schemaDB implementation is done
	// and understand how to recover from a failed remove.
	// One option is to rebuild the schema from scratch using the cache content.
	if err != nil {
		return errors.Wrapf(err, "Schema remove failed")
	}

	i := sort.Search(len(s.orderedMutators), func(i int) bool {
		res := equal(s.orderedMutators[i], m)
		if res {
			return true
		}
		return greaterOrEqual(s.orderedMutators[i], m)
	})

	delete(s.mutatorsMap, m.ID())

	found := equal(s.orderedMutators[i], m)

	// The map is expected to be in sync with the list, so if we don't find it
	// we return an error.
	if !found {
		return fmt.Errorf("Failed to find mutator with ID %v on sorted list", m.ID())
	}
	copy(s.orderedMutators[i:], s.orderedMutators[i+1:])
	s.orderedMutators[len(s.orderedMutators)-1] = nil
	s.orderedMutators = s.orderedMutators[:len(s.orderedMutators)-1]
	return nil
}

func greaterOrEqual(mutator1, mutator2 Mutator) bool {
	if mutator1.ID().Group > mutator2.ID().Group {
		return true
	}
	if mutator1.ID().Group < mutator2.ID().Group {
		return false
	}
	if mutator1.ID().Kind > mutator2.ID().Kind {
		return true
	}
	if mutator1.ID().Kind < mutator2.ID().Kind {
		return false
	}
	if mutator1.ID().Namespace > mutator2.ID().Namespace {
		return true
	}
	if mutator1.ID().Namespace < mutator2.ID().Namespace {
		return false
	}
	if mutator1.ID().Name > mutator2.ID().Name {
		return true
	}
	if mutator1.ID().Name < mutator2.ID().Name {
		return false
	}
	return true
}

func equal(mutator1, mutator2 Mutator) bool {
	if mutator1.ID().Group == mutator2.ID().Group &&
		mutator1.ID().Kind == mutator2.ID().Kind &&
		mutator1.ID().Namespace == mutator2.ID().Namespace &&
		mutator1.ID().Name == mutator2.ID().Name {
		return true
	}
	return false
}
