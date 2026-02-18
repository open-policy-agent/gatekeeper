package mutation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	mutationschema "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Leverage existing resource types to create custom mutators to validate
// the cache.
type fakeMutator struct {
	MID types.ID
	// MPath is relevant for comparison.
	// Use different values to differentiate mutators.
	MPath         parser.Path
	GVKs          []schema.GroupVersionKind
	Labels        map[string]string
	MutationCount int
	// UnstableFor makes the mutation unstable for the first n mutations.
	UnstableFor int

	// External data fields
	FailurePolicy types.ExternalDataFailurePolicy
	Default       *types.Anything
}

func (m *fakeMutator) Matches(*types.Mutable) (bool, error) {
	return true, nil // always matches
}

func (m *fakeMutator) Mutate(mutable *types.Mutable) (bool, error) {
	if m.Labels == nil {
		return false, nil
	}
	m.MutationCount++

	current := mutable.Object.GetLabels()
	if current == nil {
		current = make(map[string]string)
	}

	for k, v := range m.Labels {
		// we need to make the mutation unstable, adding the count
		if m.MutationCount < m.UnstableFor {
			v = fmt.Sprintf("%s%d", v, m.MutationCount)
		}

		current[k] = v
	}

	mutable.Object.SetLabels(current)

	return true, nil
}

func (m *fakeMutator) MustTerminate() bool {
	return false
}

func (m *fakeMutator) TerminalType() parser.NodeType {
	return mutationschema.Unknown
}

func (m *fakeMutator) ID() types.ID {
	return m.MID
}

func (m *fakeMutator) Path() parser.Path {
	return m.MPath
}

func (m *fakeMutator) Value(_ types.MetadataGetter) (interface{}, error) {
	return nil, nil
}

func (m *fakeMutator) HasDiff(mutator types.Mutator) bool {
	return !cmp.Equal(m, mutator, cmpopts.EquateEmpty())
}

func (m *fakeMutator) DeepCopy() types.Mutator {
	res := &fakeMutator{
		MID:           m.MID,
		MPath:         m.MPath.DeepCopy(),
		GVKs:          make([]schema.GroupVersionKind, len(m.GVKs)),
		MutationCount: m.MutationCount,
		UnstableFor:   m.UnstableFor,
	}
	copy(res.GVKs, m.GVKs)

	if m.Labels != nil {
		if res.Labels == nil {
			res.Labels = make(map[string]string)
		}

		for k, v := range m.Labels {
			res.Labels[k] = v
		}
	}

	return res
}

func (m *fakeMutator) String() string {
	return m.MID.String()
}

func (m *fakeMutator) SchemaBindings() []schema.GroupVersionKind {
	return m.GVKs
}

func TestMutation(t *testing.T) {
	table := []struct {
		name           string
		mutations      []*fakeMutator
		wantLabels     map[string]string
		wantIterations int
		wantErr        error
	}{
		{
			name: "mutate",
			mutations: []*fakeMutator{
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"},
					Labels: map[string]string{"ka": "va"},
				},
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"},
					Labels: map[string]string{"kb": "vb"},
				},
			},
			wantLabels: map[string]string{
				"ka": "va",
				"kb": "vb",
			},
			wantIterations: 2,
		},
		{
			name: "never converge",
			mutations: []*fakeMutator{
				{
					MID:         types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"},
					Labels:      map[string]string{"ka": "va"},
					UnstableFor: 5,
				},
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"},
					Labels: map[string]string{"kb": "vb"},
				},
			},
			wantErr: ErrNotConverging,
		},
		{
			name: "converge after 3 iterations",
			mutations: []*fakeMutator{
				{
					MID:         types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"},
					Labels:      map[string]string{"ka": "va"},
					UnstableFor: 3,
				},
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"},
					Labels: map[string]string{"kb": "vb"},
				},
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ccc"},
					Labels: map[string]string{"kb": "vb"},
				},
				{
					MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ddd"},
					Labels: map[string]string{"kb": "vb"},
				},
			},
			wantLabels: map[string]string{
				"ka": "va",
				"kb": "vb",
			},
			wantIterations: 4,
		},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			pod := fakes.Pod(
				fakes.WithNamespace("foo"),
				fakes.WithName("test-pod"),
			)

			converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
			if err != nil {
				t.Fatal("Convert pod to unstructured failed")
			}
			toMutate := &unstructured.Unstructured{Object: converted}

			c := NewSystem(SystemOpts{})

			for i, m := range tc.mutations {
				err = c.Upsert(m)
				if err != nil {
					t.Errorf("got error inserting %dth object: %v", i, err)
				}
			}

			mutated, err := c.Mutate(context.Background(), &types.Mutable{Object: toMutate})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got Mutate() error = %v, want %v", err, tc.wantErr)
			}

			// If error is expected, don't do additional checks.
			if tc.wantErr != nil {
				return
			}

			if err != nil {
				t.Fatal("Mutate failed unexpectedly", err)
			}

			newLabels := toMutate.GetLabels()

			if !mutated {
				t.Error("Mutation not as want", cmp.Diff(tc.wantLabels, newLabels))
			}

			if diff := cmp.Diff(tc.wantLabels, newLabels); diff != "" {
				t.Error("Mutation not as want", diff)
			}

			// Fetch a mock mutator to check the number of iterations.
			mID := c.orderedMutators.ids[0]
			probe, ok := c.mutatorsMap[mID].(*fakeMutator)
			if !ok {
				t.Fatalf("mutator type %T, want %T", c.orderedMutators.ids[0], &fakeMutator{})
			}

			if probe.MutationCount != tc.wantIterations {
				t.Errorf("got %d  mutation iterations, want %d", tc.mutations[0].MutationCount, tc.wantIterations)
			}
		})
	}
}

func mustParse(s string) parser.Path {
	p, err := parser.Parse(s)
	if err != nil {
		panic(err)
	}
	return p
}

func TestSystem_DontApplyConflictingMutations(t *testing.T) {
	// Two conflicting mutators.
	foo := &fakeMutator{
		MID:    types.ID{Name: "foo"},
		MPath:  mustParse("spec.containers[name: foo].image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    types.ID{Name: "foo-conflict"},
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}

	s := NewSystem(SystemOpts{})

	err := s.Upsert(foo)
	if err != nil {
		t.Fatalf("got Upsert() error = %v, want <nil>", err)
	}

	// We can mutate objects before System is put in an inconsistent state.
	t.Run("mutate works on consistent state", func(t *testing.T) {
		u := &unstructured.Unstructured{}
		gotMutated, gotErr := s.Mutate(context.Background(), &types.Mutable{
			Object:    u,
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
		})
		if !gotMutated {
			t.Errorf("got Mutate() = %t, want true", gotMutated)
		}
		if gotErr != nil {
			t.Fatalf("got Mutate() error = %v, want <nil>", gotErr)
		}
	})

	// Put System in an inconsistent state.
	err = s.Upsert(fooConflict)
	if err == nil {
		t.Fatalf("got Upsert() error = %v, want error", err)
	}

	// Since foo and foo-conflict define conflicting schemas, neither is executed.
	t.Run("no mutation on inconsistent state", func(t *testing.T) {
		u2 := &unstructured.Unstructured{}
		gotMutated, gotErr := s.Mutate(context.Background(), &types.Mutable{
			Object:    u2,
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
		})
		if gotMutated {
			t.Errorf("got Mutate() = %t, want %t", gotMutated, false)
		}

		if gotErr != nil {
			t.Fatalf("got Mutate() error = %v, want <nil>", gotErr)
		}
	})

	// Get the system back to a consistent state.
	err = s.Remove(types.ID{Name: "foo-conflict"})
	if err != nil {
		t.Fatalf("got Remove() error = %v, want <nil>", err)
	}

	// Mutations are performed again.
	t.Run("mutations performed after conflict removed", func(t *testing.T) {
		u3 := &unstructured.Unstructured{}
		gotMutated, gotErr := s.Mutate(context.Background(), &types.Mutable{
			Object:    u3,
			Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
		})
		if !gotMutated {
			t.Errorf("got Mutate() = %t, want true", gotMutated)
		}
		if gotErr != nil {
			t.Fatalf("got Mutate() error = %v, want <nil>", gotErr)
		}
	})
}

func TestSystem_DontApplyConflictingMutationsRemoveOriginal(t *testing.T) {
	// Two conflicting mutators.
	foo := &fakeMutator{
		MID:    types.ID{Name: "foo"},
		MPath:  mustParse("spec.containers[name: foo].image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    types.ID{Name: "foo-conflict"},
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}

	// Put System in an inconsistent state.
	s := NewSystem(SystemOpts{})

	err := s.Upsert(foo)
	if err != nil {
		t.Fatalf("got Upsert() error = %v, want <nil>", err)
	}
	err = s.Upsert(fooConflict)
	if err == nil {
		t.Fatalf("got Upsert() error = %v, want error", err)
	}
	gotErr := s.Remove(types.ID{Name: "foo"})
	if gotErr != nil {
		t.Fatalf("got Remove() error = %v, want <nil>", gotErr)
	}

	u := &unstructured.Unstructured{}
	gotMutated, gotErr := s.Mutate(context.Background(), &types.Mutable{
		Object:    u,
		Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
	})
	if !gotMutated {
		t.Errorf("got Mutate() = %t, want %t", gotMutated, true)
	}
	if gotErr != nil {
		t.Fatalf("got Mutate() error = %v, want %v", gotErr, nil)
	}
}

func id(name string) types.ID {
	return types.ID{Name: name}
}

func TestSystem_EarliestConflictingMutatorWins(t *testing.T) {
	// Two conflicting mutators.
	foo := &fakeMutator{
		MID:    id("foo"),
		MPath:  mustParse("spec.containers[name: foo].image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    id("foo-conflict"),
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}
	// A non-conflicting mutator on the same type.
	bar := &fakeMutator{
		MID:    id("bar"),
		MPath:  mustParse("spec.images[name: nginx].tag"),
		GVKs:   []schema.GroupVersionKind{{Version: "v1", Kind: "Pod"}},
		Labels: map[string]string{"active": "true"},
	}

	// Put System in an inconsistent state.
	s := NewSystem(SystemOpts{})
	err := s.Upsert(foo)
	if err != nil {
		t.Fatalf("got Upsert() error = %v, want <nil>", err)
	}
	err = s.Upsert(fooConflict)
	if err == nil {
		t.Fatalf("got Upsert() error = %v, want error", err)
	}
	err = s.Upsert(bar)
	if err != nil {
		t.Fatalf("got Upsert() error = %v, want <nil>", err)
	}

	u := &unstructured.Unstructured{}
	gotMutated, gotErr := s.Mutate(context.Background(), &types.Mutable{
		Object:    u,
		Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
	})
	if !gotMutated {
		t.Errorf("got Mutate() = %t, want %t", gotMutated, true)
	}
	if gotErr != nil {
		t.Fatalf("got Mutate() error = %v, want <nil>", gotErr)
	}

	if getMutationCount(t, s.Get(id("foo"))) != 0 {
		t.Errorf("got foo.MutationCount == %d, want %d", foo.MutationCount, 0)
	}
	if getMutationCount(t, s.Get(id("foo-conflict"))) != 0 {
		t.Errorf("got fooConflict.MutationCount == %d, want %d", fooConflict.MutationCount, 0)
	}
	if getMutationCount(t, s.Get(id("bar"))) != 2 {
		t.Errorf("got bar.MutationCount == %d, want %d", bar.MutationCount, 2)
	}
}

func getMutationCount(t *testing.T, m types.Mutator) int {
	f, ok := m.(*fakeMutator)
	if !ok {
		t.Fatalf("got mutator type %T, want %T", m, &fakeMutator{})
	}
	return f.MutationCount
}

type fakeReporter struct {
	called            bool
	convergenceStatus SystemConvergenceStatus
	iterations        int
}

func (fr *fakeReporter) ReportIterationConvergence(scs SystemConvergenceStatus, iterations int) error {
	fr.called = true
	fr.convergenceStatus = scs
	fr.iterations = iterations
	return nil
}

// TestSystem_ReportingInjection verifies that a system with injected reporting calls the
// reporting functions.
func TestSystem_ReportingInjection(t *testing.T) {
	// Define some mutators
	mutators := []*fakeMutator{
		{
			MID:         types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"},
			Labels:      map[string]string{"ka": "va"},
			UnstableFor: 3,
		},
		{
			MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"},
			Labels: map[string]string{"kb": "vb"},
		},
		{
			MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ccc"},
			Labels: map[string]string{"kb": "vb"},
		},
		{
			MID:    types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ddd"},
			Labels: map[string]string{"kb": "vb"},
		},
	}

	fr := &fakeReporter{}
	s := NewSystem(SystemOpts{Reporter: fr})

	for i, m := range mutators {
		err := s.Upsert(m)
		if err != nil {
			t.Errorf("Failed inserting %dth object", i)
		}
	}

	// Prepare a mutate-able object
	pod := fakes.Pod(
		fakes.WithNamespace("foo"),
		fakes.WithName("test-pod"),
	)

	converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		t.Fatal("Convert pod to unstructured failed")
	}

	toMutate := &unstructured.Unstructured{Object: converted}
	_, err = s.Mutate(context.Background(), &types.Mutable{Object: toMutate})
	if err != nil {
		t.Fatal("Mutate failed unexpectedly", err)
	}

	if !fr.called {
		t.Fatal("Reporting function was not called")
	}

	if fr.convergenceStatus != SystemConvergenceTrue {
		t.Errorf("want system to report %v but found %v", SystemConvergenceTrue, fr.convergenceStatus)
	}

	wantIterations := 4
	if fr.iterations != wantIterations {
		t.Errorf("want system to report %v iterations but found %v", wantIterations, fr.iterations)
	}
}

func TestSystem_Mutate_InverseMutations(t *testing.T) {
	// Construct Mutators which perform conflicting operations and an object which
	// will be unchanged by applying the Mutators in order.
	m1 := &fakeMutator{
		MID:    types.ID{Name: "mutation-1"},
		Labels: map[string]string{"foo": "qux"},
	}
	m2 := &fakeMutator{
		MID:    types.ID{Name: "mutation-2"},
		Labels: map[string]string{"foo": "bar"},
	}

	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{"foo": "bar"})

	s := NewSystem(SystemOpts{})
	err := s.Upsert(m1)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Upsert(m2)
	if err != nil {
		t.Fatal(err)
	}

	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj})
	if mutated {
		t.Errorf("got Mutate() = %t, want %t", mutated, false)
	}

	if err != nil {
		t.Errorf("got Mutate() error = %v, want %v", err, nil)
	}
}

func TestSystem_Upsert_ReplaceMutator(t *testing.T) {
	idFoo := types.ID{Name: "foo"}
	m := &fakeMutator{MID: idFoo}

	s := NewSystem(SystemOpts{})

	err := s.Upsert(m)
	if err != nil {
		t.Fatal(err)
	}

	m2 := &fakeMutator{MID: idFoo, MPath: mustParse("foo")}

	if diff := cmp.Diff(m2, s.mutatorsMap[idFoo], cmpopts.EquateEmpty()); diff == "" {
		t.Fatal("mutators are indistinguishable")
	}

	err = s.Upsert(m2)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(m2, s.mutatorsMap[idFoo], cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}
