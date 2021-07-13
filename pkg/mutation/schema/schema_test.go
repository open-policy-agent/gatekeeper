package schema

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ MutatorWithSchema = &fakeMutator{}

type fakeMutator struct {
	id       types.ID
	bindings []schema.GroupVersionKind
	pathStr  string
	path     parser.Path
}

func newFakeMutator(id types.ID, pathStr string, bindings ...schema.GroupVersionKind) *fakeMutator {
	path, err := parser.Parse(pathStr)
	if err != nil {
		panic(err)
	}
	return &fakeMutator{
		id:       id,
		bindings: bindings,
		pathStr:  pathStr,
		path:     path,
	}
}

func (m *fakeMutator) Matches(_ client.Object, _ *corev1.Namespace) bool {
	panic("should not be called")
}

func (m *fakeMutator) Mutate(_ *unstructured.Unstructured) (bool, error) {
	panic("should not be called")
}

func (m *fakeMutator) Value() (interface{}, error) {
	panic("should not be called")
}

func (m *fakeMutator) ID() types.ID { return m.id }

func (m *fakeMutator) HasDiff(other types.Mutator) bool {
	if m == other {
		return true
	}
	if other == nil {
		return false
	}

	o, ok := other.(*fakeMutator)
	if !ok {
		err := fmt.Errorf("unexpected mutator type %T, want %T", other, &fakeMutator{})
		panic(err)
	}
	return m.id == o.id && m.pathStr == o.pathStr
}

func (m *fakeMutator) String() string {
	return ""
}

func (m *fakeMutator) DeepCopy() types.Mutator {
	result := &fakeMutator{
		id:       m.id,
		pathStr:  m.pathStr,
		bindings: make([]schema.GroupVersionKind, len(m.bindings)),
		path:     m.path.DeepCopy(),
	}
	copy(result.bindings, m.bindings)
	return result
}

func (m *fakeMutator) SchemaBindings() []schema.GroupVersionKind {
	return m.bindings
}

func (m *fakeMutator) Path() parser.Path {
	return m.path
}

func TestDB_Upsert(t *testing.T) {
	testCases := []struct {
		name    string
		before  []MutatorWithSchema
		toAdd   MutatorWithSchema
		wantErr error
	}{
		{
			name:    "add nil mutator",
			before:  []MutatorWithSchema{},
			toAdd:   nil,
			wantErr: ErrNilMutator,
		},
		{
			name:   "add mutator",
			before: []MutatorWithSchema{},
			toAdd: newFakeMutator(id("bar"), "spec.containers[name: foo].image",
				gvk("", "v1", "Pod")),
			wantErr: nil,
		},
		{
			name: "overwrite identical mutator",
			before: []MutatorWithSchema{
				newFakeMutator(id("bar"), "spec.containers[name: foo].image",
					gvk("", "v1", "Pod")),
			},
			toAdd: newFakeMutator(id("bar"), "spec.containers[name: foo].image",
				gvk("", "v1", "Pod")),
			wantErr: nil,
		},
		{
			name: "add conflicting mutator",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.containers.image",
					gvk("", "v1", "Pod")),
			},
			toAdd: newFakeMutator(id("bar"), "spec.containers[name: foo].image",
				gvk("", "v1", "Pod")),
			wantErr: ErrConflictingSchema,
		},
		{
			name: "add conflicting mutator of different type",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.containers.image",
					gvk("", "v1", "Pod")),
			},
			toAdd: newFakeMutator(id("bar"), "spec.containers[name: foo].image",
				gvk("", "v2", "Pod")),
			wantErr: nil,
		},
		{
			name: "overwrite mutator with conflicting one",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.containers.image",
					gvk("", "v1", "Pod")),
			},
			toAdd: newFakeMutator(id("foo"), "spec.containers[name: foo].image",
				gvk("", "v1", "Pod")),
			wantErr: nil,
		},
		{
			name: "globbed list does not conflict with non-globbed list",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.containers[name: foo].image",
					gvk("", "v1", "Pod")),
			},
			toAdd: newFakeMutator(id("bar"), "spec.containers[name: *].image",
				gvk("", "v1", "Pod")),
			wantErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := New()

			for _, m := range tc.before {
				// Intentionally ignore errors here as in many cases previous Upserts
				// would have returned errors, and that behavior is not under test.
				_ = db.Upsert(m)
			}

			err := db.Upsert(tc.toAdd)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got Upsert() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDB_Remove(t *testing.T) {
	testCases := []struct {
		name               string
		before             []MutatorWithSchema
		toRemove           types.ID
		toCheck            types.ID
		wantConflictBefore bool
		wantConflictAfter  bool
	}{
		{
			name:               "remove from empty has no conflict",
			before:             []MutatorWithSchema{},
			toRemove:           id("foo"),
			toCheck:            id("bar"),
			wantConflictBefore: false,
			wantConflictAfter:  false,
		},
		{
			name: "no conflict after removing",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.name.image",
					gvk("", "v1", "Role")),
				newFakeMutator(id("bar"), "spec[name: foo].image",
					gvk("", "v1", "Role")),
			},
			toRemove:           id("bar"),
			toCheck:            id("foo"),
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "still conflict after removing",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.name.image",
					gvk("", "v1", "Role")),
				newFakeMutator(id("bar"), "spec[name: foo].image",
					gvk("", "v1", "Role")),
				newFakeMutator(id("qux"), "spec[name: foo].tag",
					gvk("", "v1", "Role")),
			},
			toRemove:           id("bar"),
			toCheck:            id("foo"),
			wantConflictBefore: true,
			wantConflictAfter:  true,
		},
		{
			name: "conflicts are not transitive",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.name.image",
					gvk("", "v1", "Role")),
				newFakeMutator(id("bar"), "spec[name: foo].image",
					gvk("", "v1", "Role"),
					gvk("", "v2", "Role")),
				newFakeMutator(id("qux"), "spec[name: foo].tag",
					gvk("", "v2", "Role")),
			},
			toRemove: id("bar"),
			// foo and bar are in conflict, but not qux.
			toCheck:            id("qux"),
			wantConflictBefore: false,
			wantConflictAfter:  false,
		},
		{
			name: "multiple conflicts are preserved",
			before: []MutatorWithSchema{
				newFakeMutator(id("foo"), "spec.name.image",
					gvk("", "v1", "Role")),
				newFakeMutator(id("bar"), "spec[name: rxc].image[tag: v1].id",
					gvk("", "v1", "Role"),
					gvk("", "v2", "Role")),
				newFakeMutator(id("qux"), "spec[name: rxc].image.tag.id",
					gvk("", "v2", "Role")),
			},
			toRemove:           id("foo"),
			toCheck:            id("qux"),
			wantConflictBefore: true,
			wantConflictAfter:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := New()

			for _, m := range tc.before {
				// Intentionally ignore errors here as in many cases previous Upserts
				// would have returned errors, and that behavior is not under test.
				_ = db.Upsert(m)
			}

			gotConflictBefore := db.HasConflicts(tc.toCheck)
			if gotConflictBefore != tc.wantConflictBefore {
				t.Errorf("before Remove got HasConflicts(%v) = %t, want %t",
					tc.toCheck, gotConflictBefore, tc.wantConflictBefore)
			}

			db.Remove(tc.toRemove)
			gotConflictAfter := db.HasConflicts(tc.toCheck)
			if gotConflictAfter != tc.wantConflictAfter {
				t.Errorf("after Remove got HasConflicts(%v) = %t, want %t",
					tc.toCheck, gotConflictAfter, tc.wantConflictAfter)
			}
		})
	}
}
