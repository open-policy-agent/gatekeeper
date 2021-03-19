package mutation

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// System keeps the list of mutations and
// provides an interface to apply mutations.
type System struct {
	schemaDB        schema.DB
	orderedMutators []types.Mutator
	mutatorsMap     map[types.ID]types.Mutator
	mux             sync.RWMutex
}

// NewSystem initializes an empty mutation system
func NewSystem() *System {
	return &System{
		schemaDB:        *schema.New(),
		orderedMutators: make([]types.Mutator, 0),
		mutatorsMap:     make(map[types.ID]types.Mutator),
	}
}

// Upsert updates or insert the given object, and returns
// an error in case of conflicts
func (s *System) Upsert(m types.Mutator) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	current, ok := s.mutatorsMap[m.ID()]
	if ok && !m.HasDiff(current) {
		return nil
	}

	toAdd := m.DeepCopy()

	// Checking schema consistency only if the mutator has schema
	if withSchema, ok := toAdd.(schema.MutatorWithSchema); ok {
		err := s.schemaDB.Upsert(withSchema)
		if err != nil {
			return errors.Wrapf(err, "Schema upsert failed for %v", m.ID())
		}
	}

	s.mutatorsMap[toAdd.ID()] = toAdd

	i := sort.Search(len(s.orderedMutators), func(i int) bool {
		return greaterOrEqual(s.orderedMutators[i].ID(), toAdd.ID())
	})

	if i == len(s.orderedMutators) { // Adding to the bottom of the list
		s.orderedMutators = append(s.orderedMutators, toAdd)
		return nil
	}

	found := equal(s.orderedMutators[i].ID(), toAdd.ID())
	if found {
		s.orderedMutators[i] = toAdd
		return nil
	}

	s.orderedMutators = append(s.orderedMutators, nil)
	copy(s.orderedMutators[i+1:], s.orderedMutators[i:])
	s.orderedMutators[i] = toAdd
	return nil
}

// Mutate applies the mutation in place to the given object. Returns
// true if a mutation was performed.
func (s *System) Mutate(obj *unstructured.Unstructured, ns *corev1.Namespace) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()
	original := obj.DeepCopy()
	maxIterations := len(s.orderedMutators) + 1

	allAppliedMutations := [][]types.Mutator{}

	for i := 0; i < maxIterations; i++ {
		appliedMutations := []types.Mutator{}
		old := obj.DeepCopy()
		for _, m := range s.orderedMutators {
			if m.Matches(obj, ns) {
				mutated, err := m.Mutate(obj)
				if mutated && *MutationLoggingEnabled {
					appliedMutations = append(appliedMutations, m)
				}
				if err != nil {
					return false, errors.Wrapf(err, "mutation %v failed for %s %s %s %s", m.ID(), obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName())
				}
			}
		}
		if cmp.Equal(old, obj) {
			if i == 0 {
				return false, nil
			}
			if cmp.Equal(original, obj) {
				if *MutationLoggingEnabled {
					logAppliedMutations("Oscillating mutation.", original, allAppliedMutations)
				}
				return false, fmt.Errorf("oscillating mutation for %s %s %s %s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName())
			}
			if *MutationLoggingEnabled {
				logAppliedMutations("Mutation applied", original, allAppliedMutations)
			}
			return true, nil
		}
		if *MutationLoggingEnabled {
			allAppliedMutations = append(allAppliedMutations, appliedMutations)
		}
	}
	if *MutationLoggingEnabled {
		logAppliedMutations("Mutation not converging", original, allAppliedMutations)
	}
	return false, fmt.Errorf("mutation not converging for %s %s %s %s", obj.GroupVersionKind().Group, obj.GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName())
}

func logAppliedMutations(message string, obj *unstructured.Unstructured, allAppliedMutations [][]types.Mutator) {
	iterations := []interface{}{}
	for i, appliedMutations := range allAppliedMutations {
		if len(appliedMutations) == 0 {
			continue
		}
		var appliedMutationsText []string
		for _, mutator := range appliedMutations {
			appliedMutationsText = append(appliedMutationsText, mutator.String())
		}
		iterations = append(iterations, fmt.Sprintf("iteration_%d", i), strings.Join(appliedMutationsText, ", "))
	}
	if len(iterations) > 0 {
		logDetails := []interface{}{}
		logDetails = append(logDetails, logging.EventType, logging.MutationApplied)
		logDetails = append(logDetails, logging.ResourceGroup, obj.GroupVersionKind().Group)
		logDetails = append(logDetails, logging.ResourceKind, obj.GroupVersionKind().Kind)
		logDetails = append(logDetails, logging.ResourceAPIVersion, obj.GroupVersionKind().Version)
		logDetails = append(logDetails, logging.ResourceNamespace, obj.GetNamespace())
		logDetails = append(logDetails, logging.ResourceName, obj.GetName())
		logDetails = append(logDetails, iterations...)
		log.Info(message, logDetails...)
	}
}

// Remove removes the mutator from the mutation system
func (s *System) Remove(id types.ID) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if _, ok := s.mutatorsMap[id]; !ok {
		return nil
	}

	s.schemaDB.Remove(id)

	i := sort.Search(len(s.orderedMutators), func(i int) bool {
		res := equal(s.orderedMutators[i].ID(), id)
		if res {
			return true
		}
		return greaterOrEqual(s.orderedMutators[i].ID(), id)
	})

	delete(s.mutatorsMap, id)

	found := equal(s.orderedMutators[i].ID(), id)

	// The map is expected to be in sync with the list, so if we don't find it
	// we return an error.
	if !found {
		return fmt.Errorf("Failed to find mutator with ID %v on sorted list", id)
	}
	copy(s.orderedMutators[i:], s.orderedMutators[i+1:])
	s.orderedMutators[len(s.orderedMutators)-1] = nil
	s.orderedMutators = s.orderedMutators[:len(s.orderedMutators)-1]
	return nil
}

// Get mutator for given id
func (s *System) Get(id types.ID) types.Mutator {
	return s.mutatorsMap[id].DeepCopy()
}

func greaterOrEqual(id1, id2 types.ID) bool {
	if id1.Group > id2.Group {
		return true
	}
	if id1.Group < id2.Group {
		return false
	}
	if id1.Kind > id2.Kind {
		return true
	}
	if id1.Kind < id2.Kind {
		return false
	}
	if id1.Namespace > id2.Namespace {
		return true
	}
	if id1.Namespace < id2.Namespace {
		return false
	}
	if id1.Name > id2.Name {
		return true
	}
	if id1.Name < id2.Name {
		return false
	}
	return true
}

func equal(id1, id2 types.ID) bool {
	if id1.Group == id2.Group &&
		id1.Kind == id2.Kind &&
		id1.Namespace == id2.Namespace &&
		id1.Name == id2.Name {
		return true
	}
	return false
}
