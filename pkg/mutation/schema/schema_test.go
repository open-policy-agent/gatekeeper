package schema

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ MutatorWithSchema = &mockMutator{}

type mockMutator struct {
	id        types.ID
	ForceDiff bool
	Bindings  []Binding
	path      string
	pathCache *parser.Path
}

func (m *mockMutator) Matches(obj runtime.Object, ns *corev1.Namespace) bool { return false }

func (m *mockMutator) Mutate(obj *unstructured.Unstructured) (bool, error) { return false, nil }

func (m *mockMutator) Value() (interface{}, error) { return nil, nil }

func (m *mockMutator) ID() types.ID { return m.id }

func (m *mockMutator) HasDiff(other types.Mutator) bool {
	if m.ForceDiff {
		return true
	}
	return !reflect.DeepEqual(m, other)
}

func (m *mockMutator) String() string {
	return ""
}

func deepCopyBindings(bindings []Binding) []Binding {
	cpy := []Binding{}
	for _, b := range bindings {
		cpy = append(cpy, Binding{
			Groups:   append([]string{}, b.Groups...),
			Kinds:    append([]string{}, b.Kinds...),
			Versions: append([]string{}, b.Versions...),
		})
	}
	return cpy
}

func (m *mockMutator) internalDeepCopy() *mockMutator {
	return &mockMutator{
		id:        m.id,
		ForceDiff: m.ForceDiff,
		Bindings:  deepCopyBindings(m.Bindings),
		path:      m.path,
	}
}

func (m *mockMutator) DeepCopy() types.Mutator {
	return m.internalDeepCopy()
}

func (m *mockMutator) SchemaBindings() []Binding { return m.Bindings }

func (m *mockMutator) Path() *parser.Path {
	if m.pathCache != nil {
		return m.pathCache
	}
	out, err := parser.Parse(m.path)
	if err != nil {
		panic(err)
	}
	m.pathCache = out
	return out
}

func id(name string) types.ID {
	return types.ID{Name: name}
}

func bindings(kinds ...string) []Binding {
	b := []Binding{}
	for _, kind := range kinds {
		b = append(b, Binding{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{kind}})
	}
	return b
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    kind,
	}
}

func sp(s string) *string {
	return &s
}

func simpleMutator(mID, kind, path string) *mockMutator {
	return &mockMutator{
		id:       id(mID),
		Bindings: bindings(kind),
		path:     path,
	}
}

func complexMutator(mID, path string, kinds ...string) *mockMutator {
	return &mockMutator{
		id:       id(mID),
		Bindings: bindings(kinds...),
		path:     path,
	}
}

var (
	basicCaseObjectLeaf       = simpleMutator("simple", "FooKind", "spec.someValue")
	basicCaseObjectLeafSchema = map[schema.GroupVersionKind]*scheme{
		gvk("FooKind"): {
			gvk: gvk("FooKind"),
			root: &node{
				referenceCount: 1,
				nodeType:       parser.ObjectNode,
				children: map[string]*node{
					"spec": {
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children:       map[string]*node{},
					},
				},
			},
		},
	}

	basicCaseListLeaf       = simpleMutator("simplelist", "FooListKind", "spec.someValue[hey: \"there\"]")
	basicCaseListLeafSchema = map[schema.GroupVersionKind]*scheme{
		gvk("FooListKind"): {
			gvk: gvk("FooListKind"),
			root: &node{
				referenceCount: 1,
				nodeType:       parser.ObjectNode,
				children: map[string]*node{
					"spec": {
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"someValue": {
								referenceCount: 1,
								nodeType:       parser.ListNode,
								keyField:       sp("hey"),
							},
						},
					},
				},
			},
		},
	}
)

func TestSchema(t *testing.T) {
	tests := []testCase{
		{
			name: "Trivial",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("trivial", "FooKind", "spec"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("trivial"): simpleMutator("trivial", "FooKind", "spec"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children:       map[string]*node{},
					},
				},
			},
		},
		{
			name: "Trivial + delete",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("trivial", "FooKind", "spec"),
				},
				{
					op: remove,
					id: id("trivial"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{},
			expectedSchemas:  map[schema.GroupVersionKind]*scheme{},
		},
		{
			name: "Simple upsert",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simple"): basicCaseObjectLeaf,
			},
			expectedSchemas: basicCaseObjectLeafSchema,
		},
		{
			name: "Add and remove simple",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("simple", "FooKind", "spec.someValue"),
				},
				{
					op: remove,
					id: id("simple"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{},
			expectedSchemas:  map[schema.GroupVersionKind]*scheme{},
		},
		{
			name: "Replace",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("simple", "FooKind", "spec.someValue"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("simple", "FooKind", "spec[someKey: *]"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simple"): simpleMutator("simple", "FooKind", "spec[someKey: *]"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ListNode,
								keyField:       sp("someKey"),
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with overshadow",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
				{
					op:      upsert,
					mutator: simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simple"):  basicCaseObjectLeaf,
				id("complex"): simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children:       map[string]*node{},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with overshadow, deleted",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
				{
					op:      upsert,
					mutator: simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
				},
				{
					op: remove,
					id: id("complex"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simple"): basicCaseObjectLeaf,
			},
			expectedSchemas: basicCaseObjectLeafSchema,
		},
		{
			name: "Simple upsert with undershadow",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
				},
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simple"):  basicCaseObjectLeaf,
				id("complex"): simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children:       map[string]*node{},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with undershadow, deleted",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
				},
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
				{
					op: remove,
					id: id("simple"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("complex"): simpleMutator("complex", "FooKind", "spec.someValue.moreValues"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children:       map[string]*node{},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with list",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
			},
			expectedSchemas: basicCaseListLeafSchema,
		},
		{
			name: "Simple upsert with list and overshadow",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
				{
					op: upsert,
					// note that we actually need to go two deep when we have a list as a terminal node
					// because a list already implies that the immediately following node is an object
					mutator: simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
				id("complex"):    simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooListKind"): {
					gvk: gvk("FooListKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 2,
										nodeType:       parser.ListNode,
										keyField:       sp("hey"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"buddy": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with list and overshadow, deleted",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
				{
					op: upsert,
					// note that we actually need to go two deep when we have a list as a terminal node
					// because a list already implies that the immediately following node is an object
					mutator: simpleMutator("complex", "FooKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
				},
				{
					op: remove,
					// note that we actually need to go two deep when we have a list as a terminal node
					// because a list already implies that the immediately following node is an object
					id: id("complex"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
			},
			expectedSchemas: basicCaseListLeafSchema,
		},
		{
			name: "Simple upsert with list and undershadow",
			ops: []testOp{
				{
					op: upsert,
					// note that we actually need to go two deep when we have a list as a terminal node
					// because a list already implies that the immediately following node is an object
					mutator: simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
				},
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
				id("complex"):    simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooListKind"): {
					gvk: gvk("FooListKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 2,
										nodeType:       parser.ListNode,
										keyField:       sp("hey"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"buddy": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with list and undershadow, deleted",
			ops: []testOp{
				{
					op: upsert,
					// note that we actually need to go two deep when we have a list as a terminal node
					// because a list already implies that the immediately following node is an object
					mutator: simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
				},
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
				{
					op: remove,
					id: id("simplelist"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("complex"): simpleMutator("complex", "FooListKind", "spec.someValue[hey: \"there\"].buddy.hallo"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooListKind"): {
					gvk: gvk("FooListKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"someValue": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("hey"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"buddy": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with branch",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("branch-b", "FooKind", "spec.common.food.hotdog"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("branch-a"): simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				id("branch-b"): simpleMutator("branch-b", "FooKind", "spec.common.food.hotdog"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"common": {
										referenceCount: 2,
										nodeType:       parser.ObjectNode,
										children: map[string]*node{
											"different": {
												referenceCount: 1,
												nodeType:       parser.ObjectNode,
												children:       map[string]*node{},
											},
											"food": {
												referenceCount: 1,
												nodeType:       parser.ObjectNode,
												children:       map[string]*node{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with branch and delete",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("branch-b", "FooKind", "spec.common.food.hotdog"),
				},
				{
					op: remove,
					id: id("branch-a"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("branch-b"): simpleMutator("branch-b", "FooKind", "spec.common.food.hotdog"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"common": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children: map[string]*node{
											"food": {
												referenceCount: 1,
												nodeType:       parser.ObjectNode,
												children:       map[string]*node{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with branch -- list",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("branch-b", "FooKind", "spec.common.food[kind: hotdog]"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("branch-a"): simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				id("branch-b"): simpleMutator("branch-b", "FooKind", "spec.common.food[kind: hotdog]"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"common": {
										referenceCount: 2,
										nodeType:       parser.ObjectNode,
										children: map[string]*node{
											"different": {
												referenceCount: 1,
												nodeType:       parser.ObjectNode,
												children:       map[string]*node{},
											},
											"food": {
												referenceCount: 1,
												keyField:       sp("kind"),
												nodeType:       parser.ListNode,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with branch -- list, deleted list",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("branch-b", "FooKind", "spec.common.food[kind: hotdog]"),
				},
				{
					op: remove,
					id: id("branch-b"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("branch-a"): simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"common": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children: map[string]*node{
											"different": {
												referenceCount: 1,
												nodeType:       parser.ObjectNode,
												children:       map[string]*node{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Simple upsert with branch -- list, deleted object",
			ops: []testOp{
				{
					op:      upsert,
					mutator: simpleMutator("branch-a", "FooKind", "spec.common.different.more"),
				},
				{
					op:      upsert,
					mutator: simpleMutator("branch-b", "FooKind", "spec.common.food[kind: hotdog]"),
				},
				{
					op: remove,
					id: id("branch-a"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("branch-b"): simpleMutator("branch-b", "FooKind", "spec.common.food[kind: hotdog]"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"common": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children: map[string]*node{
											"food": {
												referenceCount: 1,
												keyField:       sp("kind"),
												nodeType:       parser.ListNode,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multi kind, multi mutator",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
				id("simple"):     basicCaseObjectLeaf,
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooListKind"): basicCaseListLeafSchema[gvk("FooListKind")],
				gvk("FooKind"):     basicCaseObjectLeafSchema[gvk("FooKind")],
			},
		},
		{
			name: "Multi kind, multi mutator + delete",
			ops: []testOp{
				{
					op:      upsert,
					mutator: basicCaseListLeaf.internalDeepCopy(),
				},
				{
					op:      upsert,
					mutator: basicCaseObjectLeaf.internalDeepCopy(),
				},
				{
					op: remove,
					id: id("simple"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("simplelist"): basicCaseListLeaf,
			},
			expectedSchemas: basicCaseListLeafSchema,
		},
		{
			name: "Multi kind",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("many"): complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				gvk("BarKind"): {
					gvk: gvk("BarKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multi kind + delete",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				},
				{
					op: remove,
					id: id("many"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{},
			expectedSchemas:  map[schema.GroupVersionKind]*scheme{},
		},
		{
			name: "Multi kind + overlap",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				},
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "FooKind"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("many"): complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				id("one"):  complexMutator("one", "spec.trusted", "FooKind"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				gvk("BarKind"): {
					gvk: gvk("BarKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multi kind + overlap, delete simple",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				},
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "FooKind"),
				},
				{
					op: remove,
					id: id("one"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("many"): complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				gvk("BarKind"): {
					gvk: gvk("BarKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"containers": {
										referenceCount: 1,
										nodeType:       parser.ListNode,
										keyField:       sp("name"),
										child: &node{
											referenceCount: 1,
											nodeType:       parser.ObjectNode,
											children: map[string]*node{
												"property": {
													referenceCount: 1,
													nodeType:       parser.ObjectNode,
													children:       map[string]*node{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multi kind + overlap, delete complex",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("many", "spec.containers[name: sidecar].property.yep", "FooKind", "BarKind"),
				},
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "FooKind"),
				},
				{
					op: remove,
					id: id("many"),
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("one"): complexMutator("one", "spec.trusted", "FooKind"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("FooKind"): {
					gvk: gvk("FooKind"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children:       map[string]*node{},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		tester := func(t *testing.T) {
			db := New()
			testFn(test, db, t)
		}
		t.Run(test.name, tester)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name        string
		first       string
		second      string
		expectedErr string
	}{
		{
			name:        "Long path with conflict at end",
			first:       "spec.intermediate.someValue[hey: \"there\"].buddy.hallo",
			second:      "spec.intermediate.someValue[hey: again].buddy[key: *]",
			expectedErr: `spec.intermediate.someValue["hey": "again"].buddy: node type conflict: Object vs List`,
		},
		{
			name:        "Long path with conflict at end, globbed",
			first:       "spec.intermediate.someValue[hey: \"there\"].buddy.hallo",
			second:      "spec.intermediate.someValue[hey: *].buddy[key: *]",
			expectedErr: `spec.intermediate.someValue["hey": *].buddy: node type conflict: Object vs List`,
		},
		{
			name:        "Long path with conflict at beginning",
			first:       "spec.intermediate.someValue[hey: \"there\"].buddy.hallo",
			second:      "spec.intermediate[hey: again].buddy[key: *]",
			expectedErr: `spec.intermediate: node type conflict: Object vs List`,
		},
		{
			name:        "Originally list, try to add object",
			first:       "spec.containers[name: foo]",
			second:      "spec.containers.wrong",
			expectedErr: "spec.containers: node type conflict: List vs Object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := New()
			if err := db.Upsert(simpleMutator("complex", "FooKind", test.first)); err != nil {
				t.Fatal(err)
			}
			err := db.Upsert(simpleMutator("conflict", "FooKind", test.second))
			if err == nil {
				t.Fatal("unexpected nil error")
			}
			if err.Error() != test.expectedErr {
				t.Error(fmt.Sprintf("got %v, wanted %v", err.Error(), test.expectedErr))
			}
		})
	}
}

func TestUnwinding(t *testing.T) {
	tests := []struct {
		name             string
		ops              []testOp
		expectedMutators map[types.ID]MutatorWithSchema
		expectedSchemas  map[schema.GroupVersionKind]*scheme
	}{
		{
			name: "Initial Overlap",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "A"),
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec[trusted: nope]", "A", "B"),
					errorExpected: true,
					expectedError: "spec: node type conflict: Object vs List",
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("one"): complexMutator("one", "spec.trusted", "A"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("A"): {
					gvk: gvk("A"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children:       map[string]*node{},
							},
						},
					},
				},
			},
		},
		{
			name: "Second Overlap",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "B"),
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec[trusted: nope]", "A", "B"),
					errorExpected: true,
					expectedError: "spec: node type conflict: Object vs List",
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("one"): complexMutator("one", "spec.trusted", "B"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("B"): {
					gvk: gvk("B"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children:       map[string]*node{},
							},
						},
					},
				},
			},
		},
		{
			name: "Revert bad replace",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("one", "spec.trusted", "B"),
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec.trusted.yep", "A", "B"),
					errorExpected: false,
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec[trusted: nope]", "A", "B"),
					errorExpected: true,
					expectedError: "spec: node type conflict: Object vs List",
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("one"):  complexMutator("one", "spec.trusted", "B"),
				id("many"): complexMutator("many", "spec.trusted.yep", "A", "B"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("A"): {
					gvk: gvk("A"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"trusted": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children:       map[string]*node{},
									},
								},
							},
						},
					},
				},
				gvk("B"): {
					gvk: gvk("B"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ObjectNode,
								children: map[string]*node{
									"trusted": {
										referenceCount: 1,
										nodeType:       parser.ObjectNode,
										children:       map[string]*node{},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Revert bad replace -- map",
			ops: []testOp{
				{
					op:      upsert,
					mutator: complexMutator("one", "spec[trusted: *]", "B"),
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec[trusted: thing].yep", "A", "B"),
					errorExpected: false,
				},
				{
					op:            upsert,
					mutator:       complexMutator("many", "spec.trusted", "A", "B"),
					errorExpected: true,
					expectedError: "spec: node type conflict: List vs Object",
				},
			},
			expectedMutators: map[types.ID]MutatorWithSchema{
				id("one"):  complexMutator("one", "spec[trusted: *]", "B"),
				id("many"): complexMutator("many", "spec[trusted: thing].yep", "A", "B"),
			},
			expectedSchemas: map[schema.GroupVersionKind]*scheme{
				gvk("A"): {
					gvk: gvk("A"),
					root: &node{
						referenceCount: 1,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 1,
								nodeType:       parser.ListNode,
								keyField:       sp("trusted"),
								child: &node{
									referenceCount: 1,
									nodeType:       parser.ObjectNode,
									children:       map[string]*node{},
								},
							},
						},
					},
				},
				gvk("B"): {
					gvk: gvk("B"),
					root: &node{
						referenceCount: 2,
						nodeType:       parser.ObjectNode,
						children: map[string]*node{
							"spec": {
								referenceCount: 2,
								nodeType:       parser.ListNode,
								keyField:       sp("trusted"),
								child: &node{
									referenceCount: 1,
									nodeType:       parser.ObjectNode,
									children:       map[string]*node{},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		tester := func(t *testing.T) {
			db := New()
			testFn(test, db, t)
		}
		t.Run(test.name, tester)
	}
}

const (
	upsert = "upsert"
	remove = "remove"
)

type testOp struct {
	op            string
	errorExpected bool
	expectedError string
	id            types.ID
	mutator       *mockMutator
}

type testCase struct {
	name             string
	ops              []testOp
	expectedMutators map[types.ID]MutatorWithSchema
	expectedSchemas  map[schema.GroupVersionKind]*scheme
}

func testFn(test testCase, db *DB, t *testing.T) {
	for _, op := range test.ops {
		switch op.op {
		case upsert:
			err := db.Upsert(op.mutator)
			if op.errorExpected != (err != nil) {
				t.Errorf("error = %v, which is unexpected", err)
			}
			if op.expectedError != "" && err != nil && err.Error() != op.expectedError {
				t.Errorf("error = %s, expected %s", err.Error(), op.expectedError)
			}
		case remove:
			db.Remove(op.id)
		default:
			t.Error("malformed test: unrecognized op")
		}
	}
	if test.expectedSchemas != nil {
		if !reflect.DeepEqual(db.schemas, test.expectedSchemas) {
			t.Errorf("Difference in schemas: %v\n\n%s\n\n%s",
				cmp.Diff(db.schemas, test.expectedSchemas, cmp.AllowUnexported(
					scheme{},
					node{},
				)),
				spew.Sdump(db.schemas),
				spew.Sdump(test.expectedSchemas),
			)
		}
	}
	if test.expectedMutators != nil {
		if !reflect.DeepEqual(db.mutators, test.expectedMutators) {
			t.Errorf("Difference in mutators: %v", cmp.Diff(db.mutators, test.expectedMutators, cmp.AllowUnexported(
				mockMutator{},
			)))
		}
	}
}
