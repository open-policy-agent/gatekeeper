package mutation

import (
	"fmt"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ErrNotConverging reports that applying all Mutators isn't converging.
var ErrNotConverging = errors.New("mutation not converging")

// System keeps the list of mutators and provides an interface to apply mutations.
type System struct {
	schemaDB        schema.DB
	orderedMutators orderedMutators
	mutatorsMap     map[types.ID]types.Mutator
	mux             sync.RWMutex
	reporter        StatsReporter
}

// SystemOpts allows for optional dependencies to be passed into the mutation System.
type SystemOpts struct {
	Reporter StatsReporter
}

// NewSystem initializes an empty mutation system.
func NewSystem(options SystemOpts) *System {
	return &System{
		schemaDB:        *schema.New(),
		orderedMutators: orderedMutators{},
		mutatorsMap:     make(map[types.ID]types.Mutator),
		reporter:        options.Reporter,
	}
}

// Get mutator for given id.
func (s *System) Get(id types.ID) types.Mutator {
	s.mux.RLock()
	defer s.mux.RUnlock()

	mutator, found := s.mutatorsMap[id]
	if !found {
		return nil
	}
	return mutator.DeepCopy()
}

// Upsert updates or insert the given object, and returns
// an error in case of conflicts.
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
			s.schemaDB.Remove(withSchema.ID())
			return errors.Wrapf(err, "Schema upsert caused conflict for %v", m.ID())
		}
	}

	s.mutatorsMap[toAdd.ID()] = toAdd

	return s.orderedMutators.insert(toAdd)
}

// Remove removes the mutator from the mutation system.
func (s *System) Remove(id types.ID) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if _, ok := s.mutatorsMap[id]; !ok {
		return nil
	}

	s.schemaDB.Remove(id)

	delete(s.mutatorsMap, id)

	return s.orderedMutators.remove(id)
}

// Mutate applies the mutation in place to the given object. Returns
// true if a mutation was performed.
func (s *System) Mutate(obj *unstructured.Unstructured, ns *corev1.Namespace) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	mutationUUID := uuid.New()
	original := obj.DeepCopy()

	var allAppliedMutations [][]types.Mutator
	if *MutationLoggingEnabled || *MutationAnnotationsEnabled {
		allAppliedMutations = [][]types.Mutator{}
	}

	iterations := 0
	convergence := SystemConvergenceFalse
	defer func() {
		if s.reporter == nil {
			return
		}

		err := s.reporter.ReportIterationConvergence(convergence, iterations)
		if err != nil {
			log.Error(err, "failed to report mutator ingestion request")
		}
	}()

	maxIterations := len(s.orderedMutators.mutators) + 1
	for i := 0; i < maxIterations; i++ {
		iterations++
		var appliedMutations []types.Mutator
		old := obj.DeepCopy()

		for _, m := range s.orderedMutators.mutators {
			if s.schemaDB.HasConflicts(m.ID()) {
				// Don't try to apply Mutators which have conflicts.
				continue
			}

			if m.Matches(obj, ns) {
				mutated, err := m.Mutate(obj)
				if mutated {
					appliedMutations = append(appliedMutations, m)
				}
				if err != nil {
					return false, errors.Wrapf(err, "mutation %s for mutator %v failed for %s %s %s %s",
						mutationUUID,
						m.ID(),
						obj.GroupVersionKind().Group,
						obj.GroupVersionKind().Kind,
						obj.GetNamespace(),
						obj.GetName())
				}
			}
		}

		if len(appliedMutations) == 0 {
			// If no mutations were applied, we can safely assume the object is
			// identical to before.
			return i > 0, nil
		}

		if cmp.Equal(old, obj) {
			if i == 0 {
				convergence = SystemConvergenceTrue
				return false, nil
			}
			if *MutationLoggingEnabled {
				logAppliedMutations("Mutation applied", mutationUUID, original, allAppliedMutations)
			}

			if *MutationAnnotationsEnabled {
				err := mutationAnnotations(obj, allAppliedMutations, mutationUUID)
				if err != nil {
					log.Error(err, "Error applying mutation annotations", "mutation id", mutationUUID)
				}
			}

			convergence = SystemConvergenceTrue
			return true, nil
		}

		if *MutationLoggingEnabled || *MutationAnnotationsEnabled {
			allAppliedMutations = append(allAppliedMutations, appliedMutations)
		}
	}

	if *MutationLoggingEnabled {
		logAppliedMutations("Mutation not converging", mutationUUID, original, allAppliedMutations)
	}

	return false, fmt.Errorf("%w: mutation %s not converging for %s %s %s %s",
		ErrNotConverging,
		mutationUUID,
		obj.GroupVersionKind().Group,
		obj.GroupVersionKind().Kind,
		obj.GetNamespace(),
		obj.GetName())
}
