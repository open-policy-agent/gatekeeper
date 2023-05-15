package expansion

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion/fixtures"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	addOp = "UPSERT"
	rmOp  = "REMOVE"
)

type templateOperation struct {
	op       string
	template unversioned.ExpansionTemplate

	// wantErr is only relevant for add operations.
	wantErr bool
}

func TestDB(t *testing.T) {
	tests := []struct {
		name           string
		ops            []templateOperation
		wantMatchers   adjList
		wantGenerators adjList
		wantStore      map[TemplateID]*templateState
	}{
		{
			name: "add 1 template",
			ops: []templateOperation{
				{
					op:       addOp,
					template: *fixtures.TestTemplate("foo", 1, 2),
					wantErr:  false,
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): {
					template:     fixtures.TestTemplate("foo", 1, 2),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{
					Group:   "group1",
					Version: "v1",
					Kind:    "kind1",
				}: {
					keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{
					Group:   "group2",
					Version: "v2",
					Kind:    "kind2",
				}: {
					keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): true,
				},
			},
		},
		{
			name: "add 1 template that has many applyTo",
			ops: []templateOperation{
				{
					op:       addOp,
					template: *fixtures.TempMultApply(),
					wantErr:  false,
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TempMultApply()): {
					template:     fixtures.TempMultApply(),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TempMultApply()): true,
				},
				schema.GroupVersionKind{Group: "group11", Version: "v11", Kind: "kind11"}: {
					keyForTemplate(fixtures.TempMultApply()): true,
				},
				schema.GroupVersionKind{Group: "group11", Version: "v22", Kind: "kind11"}: {
					keyForTemplate(fixtures.TempMultApply()): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TempMultApply()): true,
				},
			},
		},
		{
			name: "add 2 templates with same applyTo and genGVK",
			ops: []templateOperation{
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2", 1, 2),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t1", 1, 2)): {
					template:     fixtures.TestTemplate("t1", 1, 2),
					hasConflicts: false,
				},
				keyForTemplate(fixtures.TestTemplate("t2", 1, 2)): {
					template:     fixtures.TestTemplate("t2", 1, 2),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{
					Group:   "group1",
					Version: "v1",
					Kind:    "kind1",
				}: {
					keyForTemplate(fixtures.TestTemplate("t1", 1, 2)): true,
					keyForTemplate(fixtures.TestTemplate("t2", 1, 2)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{
					Group:   "group2",
					Version: "v2",
					Kind:    "kind2",
				}: {
					keyForTemplate(fixtures.TestTemplate("t1", 1, 2)): true,
					keyForTemplate(fixtures.TestTemplate("t2", 1, 2)): true,
				},
			},
		},
		{
			name: "removing non-existing template does nothing",
			ops: []templateOperation{
				{
					op:       addOp,
					template: *fixtures.TestTemplate("foo", 1, 2),
				},
				{
					op:       rmOp,
					template: *fixtures.TestTemplate("DNE", 1, 2),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): {
					template:     fixtures.TestTemplate("foo", 1, 2),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("foo", 1, 2)): true,
				},
			},
		},
		{
			name: "update existing template",
			ops: []templateOperation{
				{
					op:       addOp,
					template: *fixtures.TestTemplate("foo", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("foo", 3, 4),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("foo", 3, 4)): {
					template:     fixtures.TestTemplate("foo", 3, 4),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("foo", 3, 4)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group4", Version: "v4", Kind: "kind4"}: {
					keyForTemplate(fixtures.TestTemplate("foo", 3, 4)): true,
				},
			},
		},
		{
			name: "adding cycle disables cyclic templates and leaves non-cyclic enabled",
			ops: []templateOperation{
				// t1 -> t2 -> t3 -> t1 -> ... forms cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-3", 2, 3),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
					wantErr:  true,
				},
				// t2-8 is "touching" the cycle, but not part of it
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-8", 2, 8),
				},
				// t5 is completely disjoint from rest of graph
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t5", 5, 6),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): {
					template:     fixtures.TestTemplate("t1-2", 1, 2),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): {
					template:     fixtures.TestTemplate("t2-3", 2, 3),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t3-1", 3, 1)): {
					template:     fixtures.TestTemplate("t3-1", 3, 1),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t2-8", 2, 8)): {
					template:     fixtures.TestTemplate("t2-8", 2, 8),
					hasConflicts: false,
				},
				keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): {
					template:     fixtures.TestTemplate("t5", 5, 6),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
					keyForTemplate(fixtures.TestTemplate("t2-8", 2, 8)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t3-1", 3, 1)): true,
				},
				schema.GroupVersionKind{Group: "group5", Version: "v5", Kind: "kind5"}: {
					keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t3-1", 3, 1)): true,
				},
				schema.GroupVersionKind{Group: "group6", Version: "v6", Kind: "kind6"}: {
					keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): true,
				},
				schema.GroupVersionKind{Group: "group8", Version: "v8", Kind: "kind8"}: {
					keyForTemplate(fixtures.TestTemplate("t2-8", 2, 8)): true,
				},
			},
		},
		{
			name: "update template to produce cycle",
			ops: []templateOperation{
				// t5 is completely disjoint from rest of graph
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t5", 5, 6),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-4", 3, 4),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-3", 2, 3),
				},
				// update 3-4 to form a cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-4", 3, 1),
					wantErr:  true,
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): {
					template:     fixtures.TestTemplate("t1-2", 1, 2),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): {
					template:     fixtures.TestTemplate("t2-3", 2, 3),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t3-4", 1, 1)): {
					template:     fixtures.TestTemplate("t3-4", 3, 1),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): {
					template:     fixtures.TestTemplate("t5", 5, 6),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t3-4", 3, 4)): true,
				},
				schema.GroupVersionKind{Group: "group5", Version: "v5", Kind: "kind5"}: {
					keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t3-4", 3, 1)): true,
				},
				schema.GroupVersionKind{Group: "group6", Version: "v6", Kind: "kind6"}: {
					keyForTemplate(fixtures.TestTemplate("t5", 5, 6)): true,
				},
			},
		},
		{
			name: "fixing cycle re-enables templates but existing cycles still disabled",
			ops: []templateOperation{
				// t7-8 and t8-7 form a cycle unconnected to the t1-t2-t3 cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t7-8", 7, 8),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t8-7", 8, 7),
					wantErr:  true,
				},
				// t1 -> t2 -> t3 -> t1 -> ... forms cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-3", 2, 3),
				},
				// t3-1 produces cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
					wantErr:  true,
				},
				// fix t3-1 to behavior like t3-4, thus fixing cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-1", 3, 4),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): {
					template:     fixtures.TestTemplate("t7-8", 7, 8),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): {
					template:     fixtures.TestTemplate("t8-7", 8, 7),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): {
					template:     fixtures.TestTemplate("t1-2", 1, 2),
					hasConflicts: false,
				},
				keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): {
					template:     fixtures.TestTemplate("t2-3", 2, 3),
					hasConflicts: false,
				},
				keyForTemplate(fixtures.TestTemplate("t3-1", 3, 4)): {
					template:     fixtures.TestTemplate("t3-1", 3, 4),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t3-1", 3, 1)): true,
				},
				schema.GroupVersionKind{Group: "group7", Version: "v7", Kind: "kind7"}: {
					keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): true,
				},
				schema.GroupVersionKind{Group: "group8", Version: "v8", Kind: "kind8"}: {
					keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group4", Version: "v4", Kind: "kind4"}: {
					keyForTemplate(fixtures.TestTemplate("t3-1", 3, 4)): true,
				},
				schema.GroupVersionKind{Group: "group8", Version: "v8", Kind: "kind8"}: {
					keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): true,
				},
				schema.GroupVersionKind{Group: "group7", Version: "v7", Kind: "kind7"}: {
					keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): true,
				},
			},
		},
		{
			name: "removing cycle re-enables templates but existing cycles still disabled",
			ops: []templateOperation{
				// t7-8 and t8-7 form a cycle unconnected to the t1-t2-t3 cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t7-8", 7, 8),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t8-7", 8, 7),
					wantErr:  true,
				},
				// t1 -> t2 -> t3 -> t1 -> ... forms cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-3", 2, 3),
				},
				// t3-1 produces cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
					wantErr:  true,
				},
				// remove t3-1 to fix cycle
				{
					op:       rmOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): {
					template:     fixtures.TestTemplate("t7-8", 7, 8),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): {
					template:     fixtures.TestTemplate("t8-7", 8, 7),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): {
					template:     fixtures.TestTemplate("t1-2", 1, 2),
					hasConflicts: false,
				},
				keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): {
					template:     fixtures.TestTemplate("t2-3", 2, 3),
					hasConflicts: false,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group7", Version: "v7", Kind: "kind7"}: {
					keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): true,
				},
				schema.GroupVersionKind{Group: "group8", Version: "v8", Kind: "kind8"}: {
					keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group8", Version: "v8", Kind: "kind8"}: {
					keyForTemplate(fixtures.TestTemplate("t7-8", 7, 8)): true,
				},
				schema.GroupVersionKind{Group: "group7", Version: "v7", Kind: "kind7"}: {
					keyForTemplate(fixtures.TestTemplate("t8-7", 8, 7)): true,
				},
			},
		},
		{
			name: "removing 1 edge from double-edged cycle does not re-enable templates",
			ops: []templateOperation{
				// t1 -> t2 -> t3 -> t1 -> ... forms cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t2-3", 2, 3),
				},
				// t3-1 produces cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
					wantErr:  true,
				},
				// ta3-1 produces double-edged cycle
				{
					op:       addOp,
					template: *fixtures.TestTemplate("ta3-1", 3, 1),
					wantErr:  true,
				},
				// remove t3-1, but cycle still exists with ta3-1
				{
					op:       rmOp,
					template: *fixtures.TestTemplate("t3-1", 3, 1),
				},
			},
			wantStore: map[TemplateID]*templateState{
				keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): {
					template:     fixtures.TestTemplate("t1-2", 1, 2),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): {
					template:     fixtures.TestTemplate("t2-3", 2, 3),
					hasConflicts: true,
				},
				keyForTemplate(fixtures.TestTemplate("ta3-1", 3, 1)): {
					template:     fixtures.TestTemplate("ta3-1", 3, 1),
					hasConflicts: true,
				},
			},
			wantMatchers: adjList{
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("ta3-1", 3, 1)): true,
				},
			},
			wantGenerators: adjList{
				schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "kind2"}: {
					keyForTemplate(fixtures.TestTemplate("t1-2", 1, 2)): true,
				},
				schema.GroupVersionKind{Group: "group3", Version: "v3", Kind: "kind3"}: {
					keyForTemplate(fixtures.TestTemplate("t2-3", 2, 3)): true,
				},
				schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"}: {
					keyForTemplate(fixtures.TestTemplate("ta3-1", 3, 1)): true,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDB()

			// Execute all the upsert/remove calls
			executeOps(d, tc.ops, t)

			if df := cmp.Diff(d.matchers, tc.wantMatchers); df != "" {
				t.Errorf("got matchers: %v\nbut want: %v\ndiff: %s", d.matchers, tc.wantMatchers, df)
			}

			if df := cmp.Diff(d.generators, tc.wantGenerators); df != "" {
				t.Errorf("got generators: %v\nbut want: %v\ndiff: %s", d.generators, tc.wantGenerators, df)
			}

			if df := cmp.Diff(d.store, tc.wantStore, cmp.AllowUnexported(templateState{})); df != "" {
				t.Errorf("got store: %v\nbut want: %v\ndiff: %s", d.generators, tc.wantGenerators, df)
			}
		})
	}
}

func executeOps(db templateDB, ops []templateOperation, t *testing.T) {
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.op {
		case addOp:
			gotErr := db.upsert(&op.template)
			if op.wantErr {
				require.Error(t, gotErr, "want err: %t, got: %s", op.wantErr, gotErr)
			} else if gotErr != nil {
				t.Errorf("unexpected err upserting: %v", gotErr)
			}
		case rmOp:
			if op.wantErr {
				t.Fatalf("cannot set errFn for remove operation")
			}
			db.remove(&op.template)
		default:
			t.Fatalf("unrecognized operation: %s", op.op)
		}
	}
}

func sortTemplates(temps []*unversioned.ExpansionTemplate) {
	sort.Slice(temps, func(i, j int) bool {
		return keyForTemplate(temps[i]) > keyForTemplate(temps[j])
	})
}

func TestTemplatesForGVK(t *testing.T) {
	tests := []struct {
		name string
		add  []*unversioned.ExpansionTemplate
		gvk  schema.GroupVersionKind
		want []*unversioned.ExpansionTemplate
	}{
		{
			name: "no templates in db returns nothing",
			gvk:  schema.GroupVersionKind{Group: "a", Version: "b", Kind: "c"},
		},
		{
			name: "2 templates no cycles",
			gvk:  schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"},
			add: []*unversioned.ExpansionTemplate{
				fixtures.TestTemplate("t1-2", 1, 2),
				fixtures.TestTemplate("t2-3", 2, 3),
			},
			want: []*unversioned.ExpansionTemplate{
				fixtures.TestTemplate("t1-2", 1, 2),
			},
		},
		{
			name: "cycle not returned",
			gvk:  schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "kind1"},
			add: []*unversioned.ExpansionTemplate{
				fixtures.TestTemplate("t1-2", 1, 2),
				fixtures.TestTemplate("t2-3", 2, 3),
				fixtures.TestTemplate("t3-1", 3, 1),
			},
		},
		{
			name: "cycle not returned but non-cyclics are",
			gvk:  schema.GroupVersionKind{Group: "group4", Version: "v4", Kind: "kind4"},
			add: []*unversioned.ExpansionTemplate{
				fixtures.TestTemplate("t1-2", 1, 2),
				fixtures.TestTemplate("t2-3", 2, 3),
				fixtures.TestTemplate("t3-1", 3, 1),
				fixtures.TestTemplate("t4-5", 4, 5),
				fixtures.TestTemplate("t4-6", 4, 6),
			},
			want: []*unversioned.ExpansionTemplate{
				fixtures.TestTemplate("t4-5", 4, 5),
				fixtures.TestTemplate("t4-6", 4, 6),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDB()
			for _, t := range tc.add {
				_ = d.upsert(t)
			}

			got := d.templatesForGVK(tc.gvk)
			sortTemplates(got)
			sortTemplates(tc.want)

			require.Len(t, got, len(tc.want))
			for i := 0; i < len(got); i++ {
				if diff := cmp.Diff(got[i], tc.want[i]); diff != "" {
					t.Errorf("got template %v\nwanted: %v\ndiff: %s", got[i], tc.want[i], diff)
				}
			}
		})
	}
}

func TestGetConflicts(t *testing.T) {
	tests := []struct {
		name string
		seed []templateState
		want map[TemplateID]bool
	}{
		{
			name: "empty db, empty conflicts",
		},
		{
			name: "2 conflicts",
			seed: []templateState{
				{
					hasConflicts: true,
					template:     fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					hasConflicts: true,
					template:     fixtures.TestTemplate("t2-1", 2, 1),
				},
			},
			want: map[TemplateID]bool{
				"t1-2": true,
				"t2-1": true,
			},
		},
		{
			name: "2 conflicts, 1 non",
			seed: []templateState{
				{
					hasConflicts: true,
					template:     fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					hasConflicts: true,
					template:     fixtures.TestTemplate("t2-1", 2, 1),
				},
				{
					hasConflicts: false,
					template:     fixtures.TestTemplate("t4-5", 4, 5),
				},
			},
			want: map[TemplateID]bool{
				"t1-2": true,
				"t2-1": true,
			},
		},
		{
			name: "no conflicts",
			seed: []templateState{
				{
					hasConflicts: false,
					template:     fixtures.TestTemplate("t1-2", 1, 2),
				},
				{
					hasConflicts: false,
					template:     fixtures.TestTemplate("t2-3", 2, 3),
				},
				{
					hasConflicts: false,
					template:     fixtures.TestTemplate("t4-5", 4, 5),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDB()
			for i := range tc.seed {
				d.store[keyForTemplate(tc.seed[i].template)] = &tc.seed[i]
			}

			got := d.getConflicts()

			require.Len(t, got, len(tc.want))
			for k := range tc.want {
				if _, exists := got[k]; !exists {
					t.Errorf("wanted template ID %s, but not returned", k)
				}
			}
		})
	}
}
