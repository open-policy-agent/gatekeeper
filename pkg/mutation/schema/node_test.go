package schema

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type idPath struct {
	types.ID
	path         string
	terminalType parser.NodeType
}

func id(name string) types.ID {
	return types.ID{Name: name}
}

func ids(names ...string) idSet {
	result := make(idSet)
	for _, n := range names {
		result[id(n)] = true
	}
	return result
}

func ip(name string, path string) idPath {
	return idPath{ID: id(name), path: path, terminalType: Unknown}
}

func ipt(name string, path string, terminalType parser.NodeType) idPath {
	return idPath{ID: id(name), path: path, terminalType: terminalType}
}

func gvk(group, version, kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
}

func TestNode_Add(t *testing.T) {
	testCases := []struct {
		name   string
		before []idPath
		add    idPath
		want   idSet
	}{
		{
			name:   "no conflict on first add",
			before: []idPath{},
			add:    ip("name", "spec.name"),
			want:   nil,
		},
		{
			name: "no conflict on different children",
			before: []idPath{
				ip("object", "spec.name"),
			},
			add:  ip("list", "spec.containers[list: foo]"),
			want: nil,
		},
		{
			name: "conflict if different key on same root",
			before: []idPath{
				ip("object", "spec.name"),
			},
			add: ip("list", "spec[list: foo]"),
			want: map[types.ID]bool{
				id("object"): true,
				id("list"):   true,
			},
		},
		{
			name: "no conflict if ambiguous list",
			before: []idPath{
				ip("object", "spec.containers"),
			},
			add:  ip("list", "spec.containers[name: foo].image"),
			want: nil,
		},
		{
			name: "no conflict if ambiguous object",
			before: []idPath{
				ip("object", "spec.containers"),
			},
			add:  ip("list", "spec.containers.image"),
			want: nil,
		},
		{
			name: "no conflict if ambiguous Set",
			before: []idPath{
				ip("object", "spec.containers"),
			},
			add:  ipt("set", "spec.containers", Set),
			want: nil,
		},
		{
			name: "list vs. object conflict",
			before: []idPath{
				ip("object", "spec.name"),
			},
			add: ip("list", "spec[name: foo]"),
			want: map[types.ID]bool{
				id("object"): true,
				id("list"):   true,
			},
		},
		{
			name: "list vs. set conflict",
			before: []idPath{
				ip("list", "spec.containers[name: foo]"),
			},
			add: ipt("set", "spec.containers", Set),
			want: map[types.ID]bool{
				id("list"): true,
				id("set"):  true,
			},
		},
		{
			name: "obj vs. set conflict",
			before: []idPath{
				ip("object", "spec.containers.name"),
			},
			add: ipt("set", "spec.containers", Set),
			want: map[types.ID]bool{
				id("object"): true,
				id("set"):    true,
			},
		},
		{
			name: "list key field conflict",
			before: []idPath{
				ip("list image", "spec[image: bar]"),
			},
			add: ip("list name", "spec[name: foo]"),
			want: map[types.ID]bool{
				id("list image"): true,
				id("list name"):  true,
			},
		},
		{
			name: "multiple conflicts",
			before: []idPath{
				ip("object-object", "spec.container.name"),
				ip("object-list", "spec.container[name: foo]"),
			},
			add: ip("list-object", "spec[container: foo].name"),
			want: map[types.ID]bool{
				id("object-object"): true,
				id("object-list"):   true,
				id("list-object"):   true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := node{}
			for _, p := range tc.before {
				path, err := parser.Parse(p.path)
				if err != nil {
					t.Fatal(err)
				}
				root.Add(p.ID, path.Nodes, p.terminalType)
			}

			path, err := parser.Parse(tc.add.path)
			if err != nil {
				t.Fatal(err)
			}
			conflicts := root.Add(tc.add.ID, path.Nodes, tc.add.terminalType)
			if diff := cmp.Diff(tc.want, conflicts); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestNode_RemovePanic(t *testing.T) {
	// Remove should panic if the expected node is not found.
	testCases := []struct {
		name      string
		before    []idPath
		toRemove  idPath
		wantPanic bool
	}{
		{
			name:      "remove from empty",
			before:    []idPath{},
			toRemove:  ip("name", "spec.name"),
			wantPanic: true,
		},
		{
			name: "remove if exists",
			before: []idPath{
				ip("name", "spec.name"),
			},
			toRemove:  ip("name", "spec.name"),
			wantPanic: false,
		},
		{
			name: "remove if other id exists",
			before: []idPath{
				ip("name", "spec.name"),
			},
			toRemove:  ip("name 2", "spec.name"),
			wantPanic: true,
		},
		{
			name: "panic if remove subpath",
			before: []idPath{
				ip("name", "spec.name"),
			},
			toRemove:  ip("name", "spec"),
			wantPanic: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := node{}
			for _, p := range tc.before {
				path, err := parser.Parse(p.path)
				if err != nil {
					t.Fatal(err)
				}
				root.Add(p.ID, path.Nodes, Unknown)
			}

			pRemove, err := parser.Parse(tc.toRemove.path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				r := recover()
				if r == nil && tc.wantPanic {
					t.Error("expected Remove to panic but did not get panic")
				} else if r != nil && !tc.wantPanic {
					t.Errorf("expected Remove to succeed but panicked: %v", r)
				}
			}()
			root.Remove(tc.toRemove.ID, pRemove.Nodes, Unknown)
		})
	}
}

func TestNode_Remove(t *testing.T) {
	testCases := []struct {
		name               string
		before             []idPath
		toRemove           []idPath
		toCheck            string
		terminalType       parser.NodeType
		wantConflictBefore bool
		wantConflictAfter  bool
	}{
		{
			name: "remove object conflict same key",
			before: []idPath{
				ip("object", "spec.name"),
				ip("list", "spec[name: foo]"),
			},
			toRemove:           []idPath{ip("object", "spec.name")},
			toCheck:            "spec[name: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "remove set conflict same key",
			before: []idPath{
				ip("object", "spec.containers.hello"),
				ipt("set", "spec.containers", Set),
			},
			toRemove:           []idPath{ipt("set", "spec.containers", Set)},
			toCheck:            "spec.containers.hello",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "remove object conflict different key",
			before: []idPath{
				ip("object", "spec.name"),
				ip("list", "spec[container: foo]"),
			},
			toRemove:           []idPath{ip("object", "spec.name")},
			toCheck:            "spec[container: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "remove list conflict",
			before: []idPath{
				ip("object", "spec.name.id"),
				ip("list", "spec[name: foo]"),
			},
			toRemove:           []idPath{ip("list", "spec[name: foo]")},
			toCheck:            "spec.name.id",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "remove list-set conflict",
			before: []idPath{
				ipt("set", "spec.containers", Set),
				ip("list", "spec.containers[name: foo]"),
			},
			toRemove:           []idPath{ipt("set", "spec.containers", Set)},
			toCheck:            "spec.containers[name: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "multiple conflicts",
			before: []idPath{
				ip("object-object", "spec.container.name"),
				ip("object-list", "spec.container[name: foo]"),
				ip("list-object", "spec[container: foo].name"),
			},
			toRemove:           []idPath{ip("list-object", "spec[container: foo].name")},
			toCheck:            "spec.container[name: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  true,
		},
		{
			name: "sublist conflict with different list keys",
			before: []idPath{
				ip("list 1", "containers[name: foo]"),
				ip("list 2", "containers[id: bar]"),
			},
			toRemove:           []idPath{ip("list 2", "containers[id: bar]")},
			toCheck:            "containers[name: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  false,
		},
		{
			name: "preserve subpath when deleting longer schema path",
			before: []idPath{
				ip("short 1", "spec.containers[name: foo]"),
				ip("long 1", "spec.containers[name: foo].image"),
				ip("short 2", "spec.containers.name"),
				ip("long 2", "spec.containers.name.image"),
			},
			toRemove: []idPath{
				ip("long 1", "spec.containers[name: foo].image"),
				ip("long 2", "spec.containers.name.image"),
			},
			toCheck:            "spec.containers[name: foo]",
			wantConflictBefore: true,
			wantConflictAfter:  true,
		},
		{
			name: "remove identical path",
			before: []idPath{
				ip("path 1", "spec.containers[name: foo]"),
				ip("path 2", "spec.containers[name: foo]"),
			},
			toRemove: []idPath{
				ip("path 1", "spec.containers[name: foo]"),
			},
			toCheck:            "spec.containers[name: foo]",
			wantConflictBefore: false,
			wantConflictAfter:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := node{}
			for _, p := range tc.before {
				path, err := parser.Parse(p.path)
				if err != nil {
					t.Fatal(err)
				}
				root.Add(p.ID, path.Nodes, p.terminalType)
			}

			pCheck, err := parser.Parse(tc.toCheck)
			if err != nil {
				t.Fatal(err)
			}
			gotConflictBefore, gotConflictBeforeErr := root.HasConflicts(pCheck.Nodes, Unknown)
			if gotConflictBefore != tc.wantConflictBefore {
				t.Errorf("before remove got HasConflicts(%q) = %t, want %t",
					tc.toCheck, gotConflictBefore, tc.wantConflictBefore)
			}
			if gotConflictBeforeErr != nil {
				t.Errorf("before remove got HasConflicts(%q) error = %v, want <nil>",
					tc.toCheck, gotConflictBeforeErr)
			}

			for _, toRemove := range tc.toRemove {
				pRemove, err := parser.Parse(toRemove.path)
				if err != nil {
					t.Fatal(err)
				}
				root.Remove(toRemove.ID, pRemove.Nodes, toRemove.terminalType)
			}

			gotConflictAfter, gotConflictAfterErr := root.HasConflicts(pCheck.Nodes, Unknown)
			if gotConflictAfter != tc.wantConflictAfter {
				t.Errorf("after remove got HasConflicts(%q) = %t, want %t",
					tc.toCheck, gotConflictAfter, tc.wantConflictAfter)
			}
			if gotConflictAfterErr != nil {
				t.Errorf("before remove got HasConflicts(%q) error = %v, want <nil>",
					tc.toCheck, gotConflictAfterErr)
			}
		})
	}
}

func TestNode_Add_Internals(t *testing.T) {
	// These tests prove the internals of node are working as expected.
	// Do not test behaviors; just validate that adding structures functions as
	// desired.

	testCases := []struct {
		name         string
		before       []string
		toAdd        string
		terminalType parser.NodeType
		want         node
	}{
		{
			name:  "just root",
			toAdd: "spec",
			want: node{
				ReferencedBy: ids("added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						Unknown: node{
							ReferencedBy: ids("added"),
						},
					},
				},
			},
		},
		{
			name: "root twice",
			before: []string{
				"spec",
			},
			toAdd: "spec",
			want: node{
				ReferencedBy: ids("0", "added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						Unknown: node{
							ReferencedBy: ids("0", "added"),
						},
					},
				},
			},
		},
		{
			name:  "object node",
			toAdd: "spec.name",
			want: node{
				ReferencedBy: ids("added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						parser.ObjectNode: node{
							ReferencedBy: ids("added"),
							Children: map[string]map[parser.NodeType]node{
								"name": {
									Unknown: node{
										ReferencedBy: ids("added"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:  "list node",
			toAdd: "spec[name: foo]",
			want: node{
				ReferencedBy: ids("added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						parser.ListNode: node{
							ReferencedBy: ids("added"),
							Children: map[string]map[parser.NodeType]node{
								"name": {
									Unknown: node{
										ReferencedBy: ids("added"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "set node",
			toAdd:        "spec.containers",
			terminalType: Set,
			want: node{
				ReferencedBy: ids("added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						parser.ObjectNode: node{
							ReferencedBy: ids("added"),
							Children: map[string]map[parser.NodeType]node{
								"containers": {
									Set: node{
										ReferencedBy: ids("added"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "conflict",
			before: []string{
				"spec.name",
			},
			toAdd: "spec[name: foo]",
			want: node{
				ReferencedBy: ids("0", "added"),
				Children: map[string]map[parser.NodeType]node{
					"spec": {
						parser.ObjectNode: node{
							ReferencedBy: ids("0"),
							Children: map[string]map[parser.NodeType]node{
								"name": {
									Unknown: node{
										ReferencedBy: ids("0"),
									},
								},
							},
						},
						parser.ListNode: node{
							ReferencedBy: ids("added"),
							Children: map[string]map[parser.NodeType]node{
								"name": {
									Unknown: node{
										ReferencedBy: ids("added"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := node{}

			for i, b := range tc.before {
				p, err := parser.Parse(b)
				if err != nil {
					t.Fatal(err)
				}
				root.Add(id(fmt.Sprint(i)), p.Nodes, Unknown)
			}
			rootBefore := *root.DeepCopy()

			p, err := parser.Parse(tc.toAdd)
			if err != nil {
				t.Fatal(err)
			}

			if tc.terminalType == parser.NodeType("") {
				tc.terminalType = Unknown
			}
			root.Add(id("added"), p.Nodes, tc.terminalType)

			if diff := cmp.Diff(tc.want, root, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}

			root.Remove(id("added"), p.Nodes, tc.terminalType)

			// We expect that adding and then removing the path causes no change.
			if diff := cmp.Diff(rootBefore, root, cmpopts.EquateEmpty()); diff != "" {
				t.Error("Add then Remove caused change", diff)
			}
		})
	}
}
