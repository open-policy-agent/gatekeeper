package mutation

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

// ErrNotConverging reports that applying all Mutators isn't converging.
var ErrNotConverging = errors.New("mutation not converging")

// ErrNotRemoved reports that we were unable to remove a Mutator properly as
// System was in an inconsistent state.
var ErrNotRemoved = errors.New("failed to find mutator on sorted list")

// System keeps the list of mutators and provides an interface to apply mutations.
type System struct {
	schemaDB                          schema.DB
	orderedMutators                   orderedIDs
	mutatorsMap                       map[types.ID]types.Mutator
	mux                               sync.RWMutex
	reporter                          StatsReporter
	newUUID                           func() uuid.UUID
	providerCache                     *externaldata.ProviderCache
	sendRequestToExternalDataProvider externaldata.SendRequestToProvider
	clientCertWatcher                 *certwatcher.CertWatcher
}

// SystemOpts allows for optional dependencies to be passed into the mutation System.
type SystemOpts struct {
	Reporter                          StatsReporter
	NewUUID                           func() uuid.UUID
	ProviderCache                     *externaldata.ProviderCache
	SendRequestToExternalDataProvider externaldata.SendRequestToProvider
	ClientCertWatcher                 *certwatcher.CertWatcher
}

// NewSystem initializes an empty mutation system.
func NewSystem(options SystemOpts) *System {
	if options.NewUUID == nil {
		options.NewUUID = uuid.New
	}

	return &System{
		schemaDB:                          *schema.New(),
		orderedMutators:                   orderedIDs{},
		mutatorsMap:                       make(map[types.ID]types.Mutator),
		reporter:                          options.Reporter,
		newUUID:                           options.NewUUID,
		providerCache:                     options.ProviderCache,
		sendRequestToExternalDataProvider: options.SendRequestToExternalDataProvider,
		clientCertWatcher:                 options.ClientCertWatcher,
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
	if m == nil {
		return schema.ErrNilMutator
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	id := m.ID()
	if current, ok := s.mutatorsMap[id]; ok && !m.HasDiff(current) {
		// Handle the case where a previous reconcile successfully updated System,
		// but the update to PodStatus failed.
		conflicts := s.schemaDB.GetConflicts(id)
		if len(conflicts) == 0 {
			return nil
		}
		return schema.NewErrConflictingSchema(conflicts)
	}

	toAdd := m.DeepCopy()

	// Check schema consistency only if the mutator has schema.
	var err error
	if withSchema, ok := toAdd.(schema.MutatorWithSchema); ok {
		err = s.schemaDB.Upsert(withSchema)

		if err != nil && !errors.As(err, &schema.ErrConflictingSchema{}) {
			// This means the error is not due to a schema conflict, and is most likely
			// a bug.
			s.schemaDB.Remove(id)
			return errors.Wrapf(err, "Schema upsert caused non-conflict error: %v", m.ID())
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

func (s *System) GetConflicts(id types.ID) map[types.ID]bool {
	return s.schemaDB.GetConflicts(id)
}

// Mutate applies the mutation in place to the given object. Returns
// true if applying Mutators caused any changes to the object.
func (s *System) Mutate(ctx context.Context, mutable *types.Mutable) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	convergence := SystemConvergenceFalse

	iterations, merr := s.mutate(ctx, mutable)
	if merr == nil {
		convergence = SystemConvergenceTrue
	}

	if s.reporter != nil {
		err := s.reporter.ReportIterationConvergence(convergence, iterations)
		if err != nil {
			log.Error(err, "failed to report mutator ingestion request")
		}
	}

	mutated := iterations != 0 && merr == nil
	return mutated, merr
}

// mutate runs all Mutators on obj. Returns the number of iterations required
// to converge, and any error encountered attempting to run Mutators.
func (s *System) mutate(ctx context.Context, mutable *types.Mutable) (int, error) {
	mutationUUID := s.newUUID()
	original := unversioned.DeepCopyWithPlaceholders(mutable.Object)
	var allAppliedMutations [][]types.Mutator
	maxIterations := len(s.orderedMutators.ids) + 1

	for iteration := 1; iteration <= maxIterations; iteration++ {
		var appliedMutations []types.Mutator
		old := unversioned.DeepCopyWithPlaceholders(mutable.Object)

		for _, id := range s.orderedMutators.ids {
			if s.schemaDB.HasConflicts(id) {
				// Don't try to apply Mutators which have conflicts.
				continue
			}

			mutator := s.mutatorsMap[id]
			matches, err := mutator.Matches(mutable)
			if err != nil {
				return iteration, matchesErr(err, mutator.ID(), mutable.Object)
			}

			if matches {
				mutated, err := mutator.Mutate(mutable)
				if mutated {
					appliedMutations = append(appliedMutations, mutator)
				}
				if err != nil {
					return iteration, mutateErr(err, mutationUUID, mutator.ID(), mutable.Object)
				}
			}
		}

		if len(appliedMutations) == 0 || cmp.Equal(old, mutable.Object) {
			// If no mutations were applied, we can safely assume the object is
			// identical to before.
			if iteration == 1 {
				return 0, nil
			}

			err := s.resolvePlaceholders(ctx, mutable.Object)
			if err != nil {
				return iteration, fmt.Errorf("failed to resolve external data placeholders: %w", err)
			}

			if *MutationLoggingEnabled {
				logAppliedMutations("Mutation applied", mutationUUID, original, allAppliedMutations, mutable.Source)
			}

			if *MutationAnnotationsEnabled {
				mutationAnnotations(mutable.Object, allAppliedMutations, mutationUUID)
			}

			return iteration, nil
		}

		if *MutationLoggingEnabled || *MutationAnnotationsEnabled {
			allAppliedMutations = append(allAppliedMutations, appliedMutations)
		}
	}

	if *MutationLoggingEnabled {
		logAppliedMutations("Mutation not converging", mutationUUID, original, allAppliedMutations, mutable.Source)
	}

	return maxIterations, fmt.Errorf("%w: mutation %s not converging for %s %s %s %s",
		ErrNotConverging,
		mutationUUID,
		mutable.Object.GroupVersionKind().Group,
		mutable.Object.GroupVersionKind().Kind,
		mutable.Object.GetNamespace(),
		getNameOrGenerateName(mutable.Object))
}

func mutateErr(err error, uid uuid.UUID, mID types.ID, obj *unstructured.Unstructured) error {
	return errors.Wrapf(err, "mutation %s for mutator %v failed for %s %s %s %s",
		uid,
		mID,
		obj.GroupVersionKind().Group,
		obj.GroupVersionKind().Kind,
		obj.GetNamespace(),
		getNameOrGenerateName(obj))
}

func matchesErr(err error, mID types.ID, obj *unstructured.Unstructured) error {
	return errors.Wrapf(err, "matching for mutator %v failed for %s %s %s %s",
		mID,
		obj.GroupVersionKind().Group,
		obj.GroupVersionKind().Kind,
		obj.GetNamespace(),
		getNameOrGenerateName(obj))
}
