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

// ErrNotRemoved reports that we were unable to remove a Mutator properly as
// System was in an inconsistent state.
var ErrNotRemoved = errors.New("failed to find mutator on sorted list")

// System keeps the list of mutators and provides an interface to apply mutations.
type System struct {
	schemaDB        schema.DB
	orderedMutators orderedIDs
	mutatorsMap     map[types.ID]types.Mutator
	mux             sync.RWMutex
	reporter        StatsReporter
	newUUID         func() uuid.UUID
}

// SystemOpts allows for optional dependencies to be passed into the mutation System.
type SystemOpts struct {
	Reporter StatsReporter
	NewUUID  func() uuid.UUID
}

// NewSystem initializes an empty mutation system.
func NewSystem(options SystemOpts) *System {
	if options.NewUUID == nil {
		options.NewUUID = uuid.New
	}

	return &System{
		schemaDB:        *schema.New(),
		orderedMutators: orderedIDs{},
		mutatorsMap:     make(map[types.ID]types.Mutator),
		reporter:        options.Reporter,
		newUUID:         options.NewUUID,
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

// Upsert updates or inserts the given object. Returns an error in case of
// schema conflicts.
func (s *System) Upsert(m types.Mutator) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	id := m.ID()
	if current, ok := s.mutatorsMap[id]; ok && !m.HasDiff(current) {
		return nil
	}

	toAdd := m.DeepCopy()

	// Check schema consistency only if the mutator has schema.
	var err error
	if withSchema, ok := toAdd.(schema.MutatorWithSchema); ok {
		err = s.schemaDB.Upsert(withSchema)

		if err != nil && !errors.As(err, &schema.ErrConflictingSchema{}) {
			s.schemaDB.Remove(id)
			return errors.Wrapf(err, "Schema upsert caused conflict for %v", m.ID())
		}
	}

	s.mutatorsMap[id] = toAdd

	s.orderedMutators.insert(id)
	return err
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

	removed := s.orderedMutators.remove(id)
	if !removed {
		return fmt.Errorf("%w: ID %v", ErrNotRemoved, id)
	}
	return nil
}

// Mutate applies the mutation in place to the given object. Returns
// true if applying Mutators caused any changes to the object.
func (s *System) Mutate(obj *unstructured.Unstructured, ns *corev1.Namespace) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	convergence := SystemConvergenceFalse

	iterations, err := s.mutate(obj, ns)
	if err == nil {
		convergence = SystemConvergenceTrue
	}

	if s.reporter != nil {
		err = s.reporter.ReportIterationConvergence(convergence, iterations)
		if err != nil {
			log.Error(err, "failed to report mutator ingestion request")
		}
	}

	mutated := iterations != 0 && err == nil
	return mutated, err
}

// mutate runs all Mutators on obj. Returns the number of iterations required
// to converge, and any error encountered attempting to run Mutators.
func (s *System) mutate(obj *unstructured.Unstructured, ns *corev1.Namespace) (int, error) {
	mutationUUID := s.newUUID()
	original := obj.DeepCopy()
	var allAppliedMutations [][]types.Mutator
	maxIterations := len(s.orderedMutators.ids) + 1

	for iteration := 1; iteration <= maxIterations; iteration++ {
		var appliedMutations []types.Mutator
		old := obj.DeepCopy()

		for _, id := range s.orderedMutators.ids {
			if s.schemaDB.HasConflicts(id) {
				// Don't try to apply Mutators which have conflicts.
				continue
			}

			m := s.mutatorsMap[id]
			if m.Matches(obj, ns) {
				mutated, err := m.Mutate(obj)
				if mutated {
					appliedMutations = append(appliedMutations, m)
				}
				if err != nil {
					return iteration, mutateErr(err, mutationUUID, m.ID(), obj)
				}
			}
		}

		if len(appliedMutations) == 0 || cmp.Equal(old, obj) {
			// If no mutations were applied, we can safely assume the object is
			// identical to before.
			if iteration == 1 {
				return 0, nil
			}

			if *MutationLoggingEnabled {
				logAppliedMutations("Mutation applied", mutationUUID, original, allAppliedMutations)
			}

			if *MutationAnnotationsEnabled {
				mutationAnnotations(obj, allAppliedMutations, mutationUUID)
			}

			return iteration, nil
		}

		if *MutationLoggingEnabled || *MutationAnnotationsEnabled {
			allAppliedMutations = append(allAppliedMutations, appliedMutations)
		}
	}

	if *MutationLoggingEnabled {
		logAppliedMutations("Mutation not converging", mutationUUID, original, allAppliedMutations)
	}

	return maxIterations, fmt.Errorf("%w: mutation %s not converging for %s %s %s %s",
		ErrNotConverging,
		mutationUUID,
		obj.GroupVersionKind().Group,
		obj.GroupVersionKind().Kind,
		obj.GetNamespace(),
		obj.GetName())
}

func mutateErr(err error, uid uuid.UUID, mID types.ID, obj *unstructured.Unstructured) error {
	return errors.Wrapf(err, "mutation %s for mutator %v failed for %s %s %s %s",
		uid,
		mID,
		obj.GroupVersionKind().Group,
		obj.GroupVersionKind().Kind,
		obj.GetNamespace(),
		obj.GetName())
}
