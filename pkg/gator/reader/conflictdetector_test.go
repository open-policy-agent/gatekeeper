package reader

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newObj(name string, ns string, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(gvk)
	u.SetNamespace(ns)
	return &u
}

func TestDetectConflicts(t *testing.T) {
	tests := []struct {
		name    string
		sources []*source
		want    []conflict
	}{
		{
			name: "2 conflicting sources",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file2.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
			},
			want: []conflict{
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						namespace: "my-ns",
						name:      "dupe",
					},
					a: &source{
						filename: "file2.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
					b: &source{
						filename: "file1.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
				},
			},
		},
		{
			name: "2 pairs of conflicting sources",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeA", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file2.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeA", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file3.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeB", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file4.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeB", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
			},
			want: []conflict{
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						name:      "dupeA",
						namespace: "my-ns",
					},
					a: &source{
						filename: "file2.yaml",
						objs: []*unstructured.Unstructured{newObj("dupeA", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
					b: &source{
						filename: "file1.yaml",
						objs: []*unstructured.Unstructured{newObj("dupeA", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
				},
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						name:      "dupeB",
						namespace: "my-ns",
					},
					a: &source{
						filename: "file4.yaml",
						objs: []*unstructured.Unstructured{newObj("dupeB", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
					b: &source{
						filename: "file3.yaml",
						objs: []*unstructured.Unstructured{newObj("dupeB", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
				},
			},
		},
		{
			name: "3 conflicting sources",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file2.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file3.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
			},
			want: []conflict{
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						namespace: "my-ns",
						name:      "dupe",
					},
					a: &source{
						filename: "file2.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
					b: &source{
						filename: "file1.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
				},
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						namespace: "my-ns",
						name:      "dupe",
					},
					a: &source{
						filename: "file3.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
					b: &source{
						filename: "file2.yaml",
						objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						})},
					},
				},
			},
		},
		{
			name: "2 sources different names",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeA", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file2.yaml",
					objs: []*unstructured.Unstructured{newObj("dupeB", "my-ns", schema.GroupVersionKind{
						Group:   "group",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
			},
		},
		{
			name: "2 sources different groups",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "groupA",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
				{
					filename: "file2.yaml",
					objs: []*unstructured.Unstructured{newObj("dupe", "my-ns", schema.GroupVersionKind{
						Group:   "groupB",
						Version: "v1",
						Kind:    "Thing",
					})},
				},
			},
		},
		{
			name: "2 conflicts within the same source",
			sources: []*source{
				{
					filename: "file1.yaml",
					objs: []*unstructured.Unstructured{
						newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						}),
						newObj("dupe", "my-ns", schema.GroupVersionKind{
							Group:   "group",
							Version: "v1",
							Kind:    "Thing",
						}),
					},
				},
			},
			want: []conflict{
				{
					id: gknn{
						GroupKind: schema.GroupKind{Group: "group", Kind: "Thing"},
						namespace: "my-ns",
						name:      "dupe",
					},
					a: &source{
						filename: "file1.yaml",
						objs: []*unstructured.Unstructured{
							newObj("dupe", "my-ns", schema.GroupVersionKind{
								Group:   "group",
								Version: "v1",
								Kind:    "Thing",
							}),
							newObj("dupe", "my-ns", schema.GroupVersionKind{
								Group:   "group",
								Version: "v1",
								Kind:    "Thing",
							}),
						},
					},
					b: &source{
						filename: "file1.yaml",
						objs: []*unstructured.Unstructured{
							newObj("dupe", "my-ns", schema.GroupVersionKind{
								Group:   "group",
								Version: "v1",
								Kind:    "Thing",
							}),
							newObj("dupe", "my-ns", schema.GroupVersionKind{
								Group:   "group",
								Version: "v1",
								Kind:    "Thing",
							}),
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectConflicts(tc.sources)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got: %v\nbut want: %v", got, tc.want)
			}
		})
	}
}
