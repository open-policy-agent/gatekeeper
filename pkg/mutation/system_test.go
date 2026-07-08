package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	mutationschema "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Leverage existing resource types to create custom mutators to validate
// the cache.

const (
	candidateTestVersionV1      = "v1"
	candidateTestKindPod        = "Pod"
	candidateTestKindConfigMap  = "ConfigMap"
	candidateTestKindDeployment = "Deployment"
	candidateTestAPIVersionKey  = "apiVersion"
	candidateTestKindKey        = "kind"
	candidateTestMetadataKey    = "metadata"
	candidateTestNameKey        = "name"
	candidateTestNamespaceKey   = "namespace"
	candidateTestDefaultNS      = "default"
	candidateTestTrueValue      = "true"
	candidateTestMatchedValue   = "matched"
	candidateTestGroupApps      = "apps"
)

type fakeMutator struct {
	MID types.ID
	// MPath is relevant for comparison.
	// Use different values to differentiate mutators.
	MPath    parser.Path
	GVKs     []schema.GroupVersionKind
	Bindings []mutationschema.Binding
	Labels   map[string]string

	MatchCount    int
	MutationCount int
	// UnstableFor makes the mutation unstable for the first n mutations.
	UnstableFor int

	NewGVK *schema.GroupVersionKind

	// External data fields
	FailurePolicy       types.ExternalDataFailurePolicy
	Default             *types.Anything
	RequiresTermination bool
}

func (m *fakeMutator) Matches(*types.Mutable) (bool, error) {
	m.MatchCount++
	return true, nil // always matches
}

func (m *fakeMutator) Mutate(mutable *types.Mutable) (bool, error) {
	if m.Labels == nil && m.NewGVK == nil {
		return false, nil
	}
	m.MutationCount++

	if m.NewGVK != nil {
		mutable.Object.SetGroupVersionKind(*m.NewGVK)
	}

	if m.Labels == nil {
		return true, nil
	}

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
	return m.RequiresTermination
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
		MID:                 m.MID,
		MPath:               m.MPath.DeepCopy(),
		GVKs:                make([]schema.GroupVersionKind, len(m.GVKs)),
		Bindings:            make([]mutationschema.Binding, len(m.Bindings)),
		MatchCount:          m.MatchCount,
		MutationCount:       m.MutationCount,
		UnstableFor:         m.UnstableFor,
		RequiresTermination: m.RequiresTermination,
	}
	copy(res.GVKs, m.GVKs)
	copy(res.Bindings, m.Bindings)
	if m.NewGVK != nil {
		gvk := *m.NewGVK
		res.NewGVK = &gvk
	}

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

func (m *fakeMutator) SchemaBindings() []mutationschema.Binding {
	if len(m.Bindings) > 0 {
		bindings := make([]mutationschema.Binding, len(m.Bindings))
		copy(bindings, m.Bindings)
		return bindings
	}

	bindings := make([]mutationschema.Binding, 0, len(m.GVKs)*4)
	for _, gvk := range m.GVKs {
		bindings = append(bindings,
			mutationschema.Binding{GVK: gvk, Operation: admissionv1.Create},
			mutationschema.Binding{GVK: gvk, Operation: admissionv1.Update},
			mutationschema.Binding{GVK: gvk, Operation: admissionv1.Delete},
			mutationschema.Binding{GVK: gvk, Operation: admissionv1.Connect},
		)
	}
	return bindings
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

func TestSystemTracksMutatorsThatMayNeedPlaceholderResolution(t *testing.T) {
	s := NewSystem(SystemOpts{})
	terminating := &fakeMutator{MID: id("terminating"), RequiresTermination: true}
	nonTerminating := &fakeMutator{MID: id("non-terminating")}

	if err := s.Upsert(terminating); err != nil {
		t.Fatalf("Upsert(terminating) error = %v", err)
	}
	if got, want := s.mustTerminateMutators, 1; got != want {
		t.Fatalf("mustTerminateMutators = %d, want %d", got, want)
	}

	if err := s.Upsert(nonTerminating); err != nil {
		t.Fatalf("Upsert(nonTerminating) error = %v", err)
	}
	if got, want := s.mustTerminateMutators, 1; got != want {
		t.Fatalf("mustTerminateMutators after non-terminating upsert = %d, want %d", got, want)
	}

	replacement := &fakeMutator{MID: id("terminating")}
	if err := s.Upsert(replacement); err != nil {
		t.Fatalf("Upsert(replacement) error = %v", err)
	}
	if got, want := s.mustTerminateMutators, 0; got != want {
		t.Fatalf("mustTerminateMutators after replacement = %d, want %d", got, want)
	}

	replacement.RequiresTermination = true
	if err := s.Upsert(replacement); err != nil {
		t.Fatalf("Upsert(terminating replacement) error = %v", err)
	}
	if err := s.Remove(replacement.ID()); err != nil {
		t.Fatalf("Remove(replacement) error = %v", err)
	}
	if got, want := s.mustTerminateMutators, 0; got != want {
		t.Fatalf("mustTerminateMutators after remove = %d, want %d", got, want)
	}
}

func TestSystem_Mutate_UsesSchemaBindingCandidates(t *testing.T) {
	podGVK := schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod}
	deploymentGVK := schema.GroupVersionKind{Group: candidateTestGroupApps, Version: candidateTestVersionV1, Kind: candidateTestKindDeployment}

	s := NewSystem(SystemOpts{})
	mutators := []*fakeMutator{
		{
			MID:    id("a-first"),
			GVKs:   []schema.GroupVersionKind{podGVK},
			Labels: map[string]string{"order": "first"},
		},
		{
			MID:    id("b-gvk-skipped"),
			GVKs:   []schema.GroupVersionKind{deploymentGVK},
			Labels: map[string]string{"skipped-gvk": candidateTestTrueValue},
		},
		{
			MID: id("c-operation-skipped"),
			Bindings: []mutationschema.Binding{{
				GVK:       podGVK,
				Operation: admissionv1.Update,
			}},
			Labels: map[string]string{"skipped-operation": candidateTestTrueValue},
		},
		{
			MID:    id("d-last"),
			GVKs:   []schema.GroupVersionKind{podGVK},
			Labels: map[string]string{"order": "last"},
		},
	}
	for _, mutator := range mutators {
		if err := s.Upsert(mutator); err != nil {
			t.Fatalf("Upsert(%s) error = %v, want <nil>", mutator.ID(), err)
		}
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(podGVK)
	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj, Operation: admissionv1.Create})
	if err != nil {
		t.Fatalf("Mutate() error = %v, want <nil>", err)
	}
	if !mutated {
		t.Fatalf("Mutate() = %t, want true", mutated)
	}

	labels := obj.GetLabels()
	if got, want := labels["order"], "last"; got != want {
		t.Fatalf("labels[order] = %q, want %q", got, want)
	}
	for _, label := range []string{"skipped-gvk", "skipped-operation"} {
		if _, ok := labels[label]; ok {
			t.Fatalf("nonmatching mutator changed object: labels = %v", labels)
		}
	}

	for _, id := range []types.ID{id("b-gvk-skipped"), id("c-operation-skipped")} {
		if got := getMatchCount(t, s.Get(id)); got != 0 {
			t.Fatalf("nonmatching mutator %s Matches calls = %d, want 0", id, got)
		}
		if got := getMutationCount(t, s.Get(id)); got != 0 {
			t.Fatalf("nonmatching mutator %s Mutate calls = %d, want 0", id, got)
		}
	}
	if got := getMutationCount(t, s.Get(id("a-first"))); got != 2 {
		t.Fatalf("first matching mutator Mutate calls = %d, want 2", got)
	}
	if got := getMutationCount(t, s.Get(id("d-last"))); got != 2 {
		t.Fatalf("last matching mutator Mutate calls = %d, want 2", got)
	}
}

func TestSystem_Mutate_PreservesEmptyOperationAndWildcardApplyToSemantics(t *testing.T) {
	podGVK := schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod}
	s := NewSystem(SystemOpts{})

	upsertAssign := func(name, location string, value string, operations ...admissionregistrationv1.OperationType) {
		t.Helper()
		assign := assign(value, location)
		assign.Name = name
		assign.Spec.ApplyTo = []match.MutationApplyTo{{
			ApplyTo: match.ApplyTo{
				Groups:   []string{podGVK.Group},
				Versions: []string{podGVK.Version},
				Kinds:    []string{podGVK.Kind},
			},
			Operations: operations,
		}}
		mutator, err := mutators.MutatorForAssign(assign)
		if err != nil {
			t.Fatalf("MutatorForAssign(%s) error = %v, want <nil>", name, err)
		}
		if err := s.Upsert(mutator); err != nil {
			t.Fatalf("Upsert(%s) error = %v, want <nil>", name, err)
		}
	}

	upsertAssign("create-only", "spec.createOnly", "bad", admissionregistrationv1.Create)
	upsertAssign("operation-wildcard", "spec.wildcard", "ok", admissionregistrationv1.OperationAll)
	upsertAssign("explicit-all", "spec.explicitAll", "ok", admissionregistrationv1.Create, admissionregistrationv1.Update)

	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	obj.SetGroupVersionKind(podGVK)
	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj})
	if err != nil {
		t.Fatalf("Mutate() error = %v, want <nil>", err)
	}
	if !mutated {
		t.Fatalf("Mutate() = %t, want true", mutated)
	}

	if got, found, err := unstructured.NestedString(obj.Object, "spec", "createOnly"); err != nil || found {
		t.Fatalf("spec.createOnly = (%q, found %t, err %v), want absent", got, found, err)
	}
	for _, field := range []string{"wildcard", "explicitAll"} {
		got, found, err := unstructured.NestedString(obj.Object, "spec", field)
		if err != nil || !found || got != "ok" {
			t.Fatalf("spec.%s = (%q, found %t, err %v), want %q", field, got, found, err, "ok")
		}
	}
}

func TestSystem_Mutate_NoSchemaMutatorsRemainCandidates(t *testing.T) {
	s := NewSystem(SystemOpts{})

	schemaMutator := &fakeMutator{
		MID:    id("schema-nonmatch"),
		GVKs:   []schema.GroupVersionKind{{Group: candidateTestGroupApps, Version: candidateTestVersionV1, Kind: candidateTestKindDeployment}},
		Labels: map[string]string{"schema": "should-not-run"},
	}
	if err := s.Upsert(schemaMutator); err != nil {
		t.Fatalf("Upsert(schema-nonmatch) error = %v, want <nil>", err)
	}

	assignMetadata := &unversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{Name: "assign-metadata"},
		Spec: unversioned.AssignMetadataSpec{
			Location: "metadata.labels.no-schema",
			Parameters: unversioned.MetadataParameters{
				Assign: makeValue("kept"),
			},
		},
	}
	noSchemaMutator, err := assignmeta.MutatorForAssignMetadata(assignMetadata)
	if err != nil {
		t.Fatalf("MutatorForAssignMetadata() error = %v, want <nil>", err)
	}
	if err := s.Upsert(noSchemaMutator); err != nil {
		t.Fatalf("Upsert(assign-metadata) error = %v, want <nil>", err)
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindConfigMap})
	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj, Operation: admissionv1.Delete})
	if err != nil {
		t.Fatalf("Mutate() error = %v, want <nil>", err)
	}
	if !mutated {
		t.Fatalf("Mutate() = %t, want true", mutated)
	}

	labels := obj.GetLabels()
	if got, want := labels["no-schema"], "kept"; got != want {
		t.Fatalf("labels[no-schema] = %q, want %q; labels = %v", got, want, labels)
	}
	if _, ok := labels["schema"]; ok {
		t.Fatalf("nonmatching schema mutator changed object: labels = %v", labels)
	}
	if got := getMatchCount(t, s.Get(id("schema-nonmatch"))); got != 0 {
		t.Fatalf("nonmatching schema mutator Matches calls = %d, want 0", got)
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
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    types.ID{Name: "foo-conflict"},
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
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
		u.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod})
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
		u2.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod})
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
		u3.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod})
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
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    types.ID{Name: "foo-conflict"},
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
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
	u.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod})
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
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
		Labels: map[string]string{"active": "true"},
	}
	fooConflict := &fakeMutator{
		MID:    id("foo-conflict"),
		MPath:  mustParse("spec.containers.image"),
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
		Labels: map[string]string{"active": "true"},
	}
	// A non-conflicting mutator on the same type.
	bar := &fakeMutator{
		MID:    id("bar"),
		MPath:  mustParse("spec.images[name: nginx].tag"),
		GVKs:   []schema.GroupVersionKind{{Version: candidateTestVersionV1, Kind: candidateTestKindPod}},
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
	u.SetGroupVersionKind(schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindPod})
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

func getMatchCount(t *testing.T, m types.Mutator) int {
	f, ok := m.(*fakeMutator)
	if !ok {
		t.Fatalf("got mutator type %T, want %T", m, &fakeMutator{})
	}
	return f.MatchCount
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

func TestMutationObjectEqualMatchesPreviousConvergenceSemantics(t *testing.T) {
	placeholder := &unversioned.ExternalDataPlaceholder{
		Ref:             &unversioned.ExternalData{Provider: "provider", FailurePolicy: types.FailurePolicyFail},
		ValueAtLocation: "old-value",
	}

	cases := []struct {
		name    string
		old     *unstructured.Unstructured
		current *unstructured.Unstructured
	}{
		{
			name:    "both nil",
			old:     nil,
			current: nil,
		},
		{
			name:    "one nil",
			old:     nil,
			current: &unstructured.Unstructured{Object: map[string]interface{}{}},
		},
		{
			name: "equal json values and placeholder",
			old: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]interface{}{
					"string":      "value",
					"int":         int64(1),
					"float":       float64(1.5),
					"bool":        true,
					"number":      json.Number("2"),
					"placeholder": placeholder,
				},
			}},
			current: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]interface{}{
					"string":      "value",
					"int":         int64(1),
					"float":       float64(1.5),
					"bool":        true,
					"number":      json.Number("2"),
					"placeholder": placeholder.DeepCopy(),
				},
			}},
		},
		{
			name: "different placeholder value",
			old: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{"placeholder": placeholder},
			}},
			current: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{"placeholder": &unversioned.ExternalDataPlaceholder{
					Ref:             placeholder.Ref.DeepCopy(),
					ValueAtLocation: "new-value",
				}},
			}},
		},
		{
			name: "nil and empty maps remain different",
			old: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}(nil),
			}},
			current: &unstructured.Unstructured{Object: map[string]interface{}{
				"spec": map[string]interface{}{},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mutationObjectEqual(tc.old, tc.current)
			want := cmp.Equal(tc.old, tc.current)
			if got != want {
				t.Fatalf("mutationObjectEqual() = %t, want previous cmp.Equal semantics %t", got, want)
			}
		})
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

func TestSystem_Mutate_RecomputesCandidatesAfterGVKChange(t *testing.T) {
	configMapGVK := schema.GroupVersionKind{Version: candidateTestVersionV1, Kind: candidateTestKindConfigMap}
	deploymentGVK := schema.GroupVersionKind{Group: candidateTestGroupApps, Version: candidateTestVersionV1, Kind: candidateTestKindDeployment}

	s := NewSystem(SystemOpts{})
	gvkChanger := &fakeMutator{
		MID:    id("a-gvk-changer"),
		GVKs:   []schema.GroupVersionKind{configMapGVK},
		NewGVK: &deploymentGVK,
	}
	deploymentMutator := &fakeMutator{
		MID:    id("b-deployment"),
		GVKs:   []schema.GroupVersionKind{deploymentGVK},
		Labels: map[string]string{"after-gvk-change": candidateTestMatchedValue},
	}
	if err := s.Upsert(gvkChanger); err != nil {
		t.Fatalf("Upsert(gvkChanger) error = %v, want <nil>", err)
	}
	if err := s.Upsert(deploymentMutator); err != nil {
		t.Fatalf("Upsert(deploymentMutator) error = %v, want <nil>", err)
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	obj.SetGroupVersionKind(configMapGVK)
	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj, Operation: admissionv1.Create})
	if err != nil {
		t.Fatalf("Mutate() error = %v, want <nil>", err)
	}
	if !mutated {
		t.Fatalf("Mutate() = %t, want true", mutated)
	}
	if got := obj.GroupVersionKind(); got != deploymentGVK {
		t.Fatalf("GVK = %s, want %s", got, deploymentGVK)
	}
	if got, want := obj.GetLabels()["after-gvk-change"], candidateTestMatchedValue; got != want {
		t.Fatalf("label after-gvk-change = %q, want %q", got, want)
	}
	if got := getMutationCount(t, s.Get(id("b-deployment"))); got == 0 {
		t.Fatalf("deployment mutator Mutate calls = %d, want > 0", got)
	}
}

func TestMutationCandidateIndexIncludesMatchingApplyTo(t *testing.T) {
	s := NewSystem(SystemOpts{})
	a := assign("matched", "spec.matched")
	a.Name = "configmap-assign"
	a.Spec.ApplyTo = []match.MutationApplyTo{{
		ApplyTo: match.ApplyTo{
			Groups:   []string{""},
			Versions: []string{"v1"},
			Kinds:    []string{candidateTestKindConfigMap},
		},
	}}
	m, err := mutators.MutatorForAssign(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(m); err != nil {
		t.Fatal(err)
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		candidateTestAPIVersionKey: candidateTestVersionV1,
		candidateTestKindKey:       candidateTestKindConfigMap,
		candidateTestMetadataKey: map[string]interface{}{
			candidateTestNameKey:      "cm",
			candidateTestNamespaceKey: candidateTestDefaultNS,
		},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: candidateTestVersionV1, Kind: candidateTestKindConfigMap})

	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj, Operation: admissionv1.Create})
	if err != nil {
		t.Fatal(err)
	}
	if !mutated {
		t.Fatal("Mutate() = false, want matching mutator to apply")
	}
	got, found, err := unstructured.NestedString(obj.Object, "spec", "matched")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "matched" {
		t.Fatalf("spec.matched = %q, found %t; want matched", got, found)
	}
}

func TestMutationCandidateIndexSkipsNonMatchingApplyTo(t *testing.T) {
	id := types.ID{Group: "mutations.gatekeeper.sh", Kind: "Assign", Name: "deployment-only"}
	nonmatching := &fakeMutator{
		MID: id,
		Bindings: []mutationschema.Binding{{
			GVK:       schema.GroupVersionKind{Group: candidateTestGroupApps, Version: candidateTestVersionV1, Kind: candidateTestKindDeployment},
			Operation: admissionv1.Create,
		}},
	}

	s := NewSystem(SystemOpts{})
	if err := s.Upsert(nonmatching); err != nil {
		t.Fatal(err)
	}

	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		candidateTestAPIVersionKey: candidateTestVersionV1,
		candidateTestKindKey:       candidateTestKindConfigMap,
		candidateTestMetadataKey: map[string]interface{}{
			candidateTestNameKey:      "cm",
			candidateTestNamespaceKey: candidateTestDefaultNS,
		},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: candidateTestVersionV1, Kind: candidateTestKindConfigMap})

	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj, Operation: admissionv1.Create})
	if err != nil {
		t.Fatal(err)
	}
	if mutated {
		t.Fatal("Mutate() = true, want false")
	}
	stored, ok := s.mutatorsMap[id].(*fakeMutator)
	if !ok {
		t.Fatalf("stored mutator type %T, want *fakeMutator", s.mutatorsMap[id])
	}
	if stored.MatchCount != 0 {
		t.Fatalf("nonmatching mutator MatchCount = %d, want 0", stored.MatchCount)
	}
}
