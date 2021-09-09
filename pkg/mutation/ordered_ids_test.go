package mutation

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

func fake(id types.ID) types.Mutator {
	return &fakeMutator{MID: id}
}

func TestOrderedMutators_Upsert(t *testing.T) {
	testCases := []struct {
		name   string
		before []types.ID
		upsert types.Mutator
		want   []types.ID
	}{
		{
			name:   "add to empty",
			before: nil,
			upsert: fake(types.ID{Name: "foo"}),
			want: []types.ID{
				{Name: "foo"},
			},
		},
		{
			name: "add to beginning",
			before: []types.ID{
				{Name: "foo"},
			},
			upsert: fake(types.ID{Name: "bar"}),
			want: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
			},
		},
		{
			name: "add to end",
			before: []types.ID{
				{Name: "bar"},
			},
			upsert: fake(types.ID{Name: "qux"}),
			want: []types.ID{
				{Name: "bar"},
				{Name: "qux"},
			},
		},
		{
			name: "insert in middle",
			before: []types.ID{
				{Name: "bar"},
				{Name: "qux"},
			},
			upsert: fake(types.ID{Name: "foo"}),
			want: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
				{Name: "qux"},
			},
		},
		{
			name: "insert duplicate",
			before: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
				{Name: "qux"},
			},
			upsert: fake(types.ID{Name: "foo"}),
			want: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
				{Name: "qux"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSystem(SystemOpts{})

			for _, b := range tc.before {
				err := s.Upsert(fake(b))
				if err != nil {
					t.Fatal(err)
				}
			}

			err := s.Upsert(tc.upsert)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tc.want, s.orderedMutators.ids); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestOrderedMutators_Remove(t *testing.T) {
	testCases := []struct {
		name   string
		before []types.ID
		remove types.ID
		want   []types.ID
	}{
		{
			name:   "remove from empty",
			before: nil,
			remove: types.ID{Name: "foo"},
			want:   nil,
		},
		{
			name: "remove from beginning",
			before: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
			},
			remove: types.ID{Name: "bar"},
			want: []types.ID{
				{Name: "foo"},
			},
		},
		{
			name: "remove from to end",
			before: []types.ID{
				{Name: "bar"},
				{Name: "qux"},
			},
			remove: types.ID{Name: "qux"},
			want: []types.ID{
				{Name: "bar"},
			},
		},
		{
			name: "remove from middle",
			before: []types.ID{
				{Name: "bar"},
				{Name: "foo"},
				{Name: "qux"},
			},
			remove: types.ID{Name: "foo"},
			want: []types.ID{
				{Name: "bar"},
				{Name: "qux"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSystem(SystemOpts{})

			for _, b := range tc.before {
				err := s.Upsert(fake(b))
				if err != nil {
					t.Fatal(err)
				}
			}

			err := s.Remove(tc.remove)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOrderedMutators_Ordering(t *testing.T) {
	mutators := []types.ID{
		{Group: "1", Kind: "1", Namespace: "1", Name: "2"},
		{Group: "1", Kind: "1", Namespace: "3", Name: "1"},
		{Group: "1", Kind: "4", Namespace: "1", Name: "1"},
		{Group: "2", Kind: "1", Namespace: "1", Name: "1"},
		{Group: "1", Kind: "1", Namespace: "1", Name: "1"},
	}

	s := NewSystem(SystemOpts{})

	for _, m := range mutators {
		err := s.Upsert(fake(m))
		if err != nil {
			t.Fatal(err)
		}
	}

	want := []types.ID{
		{Group: "1", Kind: "1", Namespace: "1", Name: "1"},
		{Group: "1", Kind: "1", Namespace: "1", Name: "2"},
		{Group: "1", Kind: "1", Namespace: "3", Name: "1"},
		{Group: "1", Kind: "4", Namespace: "1", Name: "1"},
		{Group: "2", Kind: "1", Namespace: "1", Name: "1"},
	}

	if diff := cmp.Diff(want, s.orderedMutators.ids); diff != "" {
		t.Error(diff)
	}
}

func TestOrderedMutators_InconsistentState(t *testing.T) {
	idBar := types.ID{Name: "bar"}
	m := fake(idBar)

	s := NewSystem(SystemOpts{})

	s.mutatorsMap[idBar] = m

	err := s.Remove(idBar)
	if !errors.Is(err, ErrNotRemoved) {
		t.Errorf("got Remove() error = %v, want %v", err, ErrNotRemoved)
	}
}
