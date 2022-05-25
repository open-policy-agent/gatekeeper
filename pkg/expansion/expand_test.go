package expansion

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

func newUnstructDeployment(name string, image string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Deployment",
			"apiVersion": "apps/v1",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"app": image,
				},
			},
			"spec": map[string]interface{}{
				"replicas": "3",
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app": image,
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  image,
								"image": image + ":1.14.2",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": "80",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newUnstructTemplate(data *templateData, t *testing.T) *unstructured.Unstructured {
	temp := newTemplate(data)
	u, err := objectToUnstruct(temp, temp.GroupVersionKind())
	if err != nil {
		t.Fatalf("error converting template to unstructured: %s", err)
	}
	return &u
}

func newUnstructAssign(data *assignData, t *testing.T) *unstructured.Unstructured {
	a := assignFromData(data)
	u, err := objectToUnstruct(&a, a.GroupVersionKind())
	if err != nil {
		t.Fatalf("error converting assign to unstructured: %s", err)
	}
	return &u
}

func newUnstructAssignMetadata(data *assignMetadataData, t *testing.T) *unstructured.Unstructured {
	a := assignMetadataFromData(data)
	u, err := objectToUnstruct(&a, a.GroupVersionKind())
	if err != nil {
		t.Fatalf("error converting assignmetadata to unstructured: %s", err)
	}
	return &u
}

func newUnstructModifySet(data *modifySetData, t *testing.T) *unstructured.Unstructured {
	ms := modifySetFromData(data)
	u, err := objectToUnstruct(&ms, ms.GroupVersionKind())
	if err != nil {
		t.Fatalf("error converting modifyset to unstructured: %s", err)
	}
	return &u
}

func objectToUnstruct(obj runtime.Object, gvk schema.GroupVersionKind) (unstructured.Unstructured, error) {
	s := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme, json.SerializerOptions{Yaml: true})
	u := unstructured.Unstructured{}
	var buf bytes.Buffer

	err := s.Encode(obj, &buf)
	if err != nil {
		return u, fmt.Errorf("error encoding obj: %s", err)
	}
	_, _, err = s.Decode(buf.Bytes(), &gvk, &u)
	if err != nil {
		return u, fmt.Errorf("error decoding encoded obj: %s", err)
	}

	return u, nil
}

func TestConvertTemplateExpansion(t *testing.T) {
	tests := []struct {
		name     string
		unstruct *unstructured.Unstructured
		want     *mutationsunversioned.TemplateExpansion
		wantErr  bool
	}{
		{
			name: "convert valid template expansion",
			unstruct: newUnstructTemplate(&templateData{
				name: "test1",
				apply: []match.ApplyTo{{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				}},
				source: "spec.template",
				generatedGVK: mutationsunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}, t),
			want: newTemplate(&templateData{
				name: "test1",
				apply: []match.ApplyTo{{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				}},
				source: "spec.template",
				generatedGVK: mutationsunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}),
		},
		{
			name:     "nil template expansion produces error",
			unstruct: nil,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertTemplateExpansion(tc.unstruct)
			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("expected error, got nil")
			case !tc.wantErr && err != nil:
				t.Fatalf("unexpected error calling convertTemplateExpansion: %s", err)
			case tc.wantErr:
				return
			}

			diff := cmp.Diff(&got, tc.want)
			if diff != "" {
				t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(got), prettyResource(tc.want), diff)
			}
		})
	}
}

func TestConvertAssign(t *testing.T) {
	tests := []struct {
		name     string
		unstruct *unstructured.Unstructured
		want     mutationsunversioned.Assign
		wantErr  bool
	}{
		{
			name: "convert valid assign",
			unstruct: newUnstructAssign(&assignData{
				name: "always-pull-image-pods",
				apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				location: "spec.containers[name: *].imagePullPolicy",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
				},
				parameters: mutationsunversioned.Parameters{
					Assign: mutationsunversioned.AssignField{
						Value: &types.Anything{Value: "Always"},
					},
				},
			}, t),
			want: assignFromData(&assignData{
				name: "always-pull-image-pods",
				apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				location: "spec.containers[name: *].imagePullPolicy",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
				},
				parameters: mutationsunversioned.Parameters{
					Assign: mutationsunversioned.AssignField{
						Value: &types.Anything{Value: "Always"},
					},
				},
			}),
		},
		{
			name:     "nil assign produces error",
			unstruct: nil,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertAssign(tc.unstruct)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error calling convertAssign: %s", err)
			}

			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(got), prettyResource(tc.want), diff)
			}
		})
	}
}

func TestConvertAssignMetadata(t *testing.T) {
	tests := []struct {
		name     string
		unstruct *unstructured.Unstructured
		want     mutationsunversioned.AssignMetadata
		wantErr  bool
	}{
		{
			name: "convert valid assignmetadata",
			unstruct: newUnstructAssignMetadata(&assignMetadataData{
				name: "add-annotation",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
					Kinds: []match.Kinds{{
						APIGroups: []string{"*"},
						Kinds:     []string{"Pod"},
					}},
				},
				location: "metadata.annotations.owner",
				parameters: mutationsunversioned.MetadataParameters{
					Assign: mutationsunversioned.AssignField{
						Value: &types.Anything{Value: "admin"},
					},
				},
			}, t),
			want: assignMetadataFromData(&assignMetadataData{
				name: "add-annotation",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
					Kinds: []match.Kinds{{
						APIGroups: []string{"*"},
						Kinds:     []string{"Pod"},
					}},
				},
				location: "metadata.annotations.owner",
				parameters: mutationsunversioned.MetadataParameters{
					Assign: mutationsunversioned.AssignField{
						Value: &types.Anything{Value: "admin"},
					},
				},
			}),
		},
		{
			name:     "nil assignmetadata produces error",
			unstruct: nil,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertAssignMetadata(tc.unstruct)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error calling convertAssign: %s", err)
			}

			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(got), prettyResource(tc.want), diff)
			}
		})
	}
}

func TestConvertModifySet(t *testing.T) {
	tests := []struct {
		name     string
		unstruct *unstructured.Unstructured
		want     mutationsunversioned.ModifySet
		wantErr  bool
	}{
		{
			name: "convert valid modifyset",
			unstruct: newUnstructModifySet(&modifySetData{
				name: "remove-err-logging",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
				},
				location: "spec.containers[name: *].args",
				apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				parameters: mutationsunversioned.ModifySetParameters{
					Operation: mutationsunversioned.MergeOp,
					Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
				},
			}, t),
			want: modifySetFromData(&modifySetData{
				name: "remove-err-logging",
				match: match.Match{
					Origin: "Generated",
					Scope:  "Cluster",
				},
				location: "spec.containers[name: *].args",
				apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				parameters: mutationsunversioned.ModifySetParameters{
					Operation: mutationsunversioned.MergeOp,
					Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
				},
			}),
		},
		{
			name:     "nil modifyset produces error",
			unstruct: nil,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertModifySet(tc.unstruct)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error calling convertModifySet: %s", err)
			}

			diff := cmp.Diff(got, tc.want)
			if diff != "" {
				t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(got), prettyResource(tc.want), diff)
			}
		})
	}
}

func TestExpandResources(t *testing.T) {
	tests := []struct {
		name      string
		resources []*unstructured.Unstructured
		want      []*unstructured.Unstructured
		wantErr   bool
	}{
		{
			name:      "empty input",
			resources: []*unstructured.Unstructured{},
			want:      []*unstructured.Unstructured{},
		},
		{
			name: "expand 1 deployment, 1 assign mutator, 1 template",
			resources: []*unstructured.Unstructured{
				newUnstructDeployment("test-deployment", "nginx"),
				newUnstructTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, t),
				newUnstructAssign(&assignData{
					name: "always-pull-image-pods",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					location: "spec.containers[name: *].imagePullPolicy",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					parameters: mutationsunversioned.Parameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "Always"},
						},
					},
				}, t),
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind":       "Pod",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": "nginx",
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":            "nginx",
									"image":           "nginx:1.14.2",
									"imagePullPolicy": "Always",
									"ports": []interface{}{
										map[string]interface{}{
											"containerPort": "80",
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
			name: "expand 1 deployment, 1 assign, 1 assignmeta, 1 modifyset, 1 template",
			resources: []*unstructured.Unstructured{
				newUnstructDeployment("test-deployment", "nginx"),
				newUnstructTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, t),
				newUnstructAssign(&assignData{
					name: "always-pull-image-pods",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					location: "spec.containers[name: *].imagePullPolicy",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					parameters: mutationsunversioned.Parameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "Always"},
						},
					},
				}, t),
				newUnstructAssignMetadata(&assignMetadataData{
					name: "add-annotation",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
						Kinds: []match.Kinds{{
							APIGroups: []string{"*"},
							Kinds:     []string{"Pod"},
						}},
					},
					location: "metadata.annotations.owner",
					parameters: mutationsunversioned.MetadataParameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "admin"},
						},
					},
				}, t),
				newUnstructModifySet(&modifySetData{
					name: "remove-err-logging",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					location: "spec.containers[name: *].args",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					parameters: mutationsunversioned.ModifySetParameters{
						Operation: mutationsunversioned.MergeOp,
						Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
					},
				}, t),
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind":       "Pod",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": "nginx",
							},
							"annotations": map[string]interface{}{
								"owner": "admin",
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":            "nginx",
									"image":           "nginx:1.14.2",
									"imagePullPolicy": "Always",
									"args": []interface{}{
										"--alsologtostderr",
									},
									"ports": []interface{}{
										map[string]interface{}{
											"containerPort": "80",
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
			name: "expand 1 deployment, 2 matching mutators, 1 non matching mutator, 1 template",
			resources: []*unstructured.Unstructured{
				newUnstructDeployment("test-deployment", "nginx"),
				newUnstructTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, t),
				newUnstructAssign(&assignData{
					name: "always-pull-image-pods",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					location: "spec.containers[name: *].imagePullPolicy",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					parameters: mutationsunversioned.Parameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "Always"},
						},
					},
				}, t),
				newUnstructAssignMetadata(&assignMetadataData{
					name: "add-annotation",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
						Kinds: []match.Kinds{{
							APIGroups: []string{"*"},
							Kinds:     []string{"NotAPod"},
						}},
					},
					location: "metadata.annotations.owner",
					parameters: mutationsunversioned.MetadataParameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "admin"},
						},
					},
				}, t),
				newUnstructModifySet(&modifySetData{
					name: "remove-err-logging",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					location: "spec.containers[name: *].args",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					parameters: mutationsunversioned.ModifySetParameters{
						Operation: mutationsunversioned.MergeOp,
						Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
					},
				}, t),
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind":       "Pod",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": "nginx",
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":            "nginx",
									"image":           "nginx:1.14.2",
									"imagePullPolicy": "Always",
									"args": []interface{}{
										"--alsologtostderr",
									},
									"ports": []interface{}{
										map[string]interface{}{
											"containerPort": "80",
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
			name: "expand 2 deployment, 1 assign, 1 assignmeta, 1 modifyset, 1 template",
			resources: []*unstructured.Unstructured{
				newUnstructDeployment("test-deployment", "nginx"),
				newUnstructDeployment("test-deployment2", "redis"),
				newUnstructTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, t),
				newUnstructAssign(&assignData{
					name: "always-pull-image-pods",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					location: "spec.containers[name: *].imagePullPolicy",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					parameters: mutationsunversioned.Parameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "Always"},
						},
					},
				}, t),
				newUnstructAssignMetadata(&assignMetadataData{
					name: "add-annotation",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
						Kinds: []match.Kinds{{
							APIGroups: []string{"*"},
							Kinds:     []string{"Pod"},
						}},
					},
					location: "metadata.annotations.owner",
					parameters: mutationsunversioned.MetadataParameters{
						Assign: mutationsunversioned.AssignField{
							Value: &types.Anything{Value: "admin"},
						},
					},
				}, t),
				newUnstructModifySet(&modifySetData{
					name: "remove-err-logging",
					match: match.Match{
						Origin: "Generated",
						Scope:  "Cluster",
					},
					location: "spec.containers[name: *].args",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					parameters: mutationsunversioned.ModifySetParameters{
						Operation: mutationsunversioned.MergeOp,
						Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
					},
				}, t),
			},
			want: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind":       "Pod",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": "nginx",
							},
							"annotations": map[string]interface{}{
								"owner": "admin",
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":            "nginx",
									"image":           "nginx:1.14.2",
									"imagePullPolicy": "Always",
									"args": []interface{}{
										"--alsologtostderr",
									},
									"ports": []interface{}{
										map[string]interface{}{
											"containerPort": "80",
										},
									},
								},
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind":       "Pod",
						"apiVersion": "v1",
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app": "redis",
							},
							"annotations": map[string]interface{}{
								"owner": "admin",
							},
						},
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":            "redis",
									"image":           "redis:1.14.2",
									"imagePullPolicy": "Always",
									"args": []interface{}{
										"--alsologtostderr",
									},
									"ports": []interface{}{
										map[string]interface{}{
											"containerPort": "80",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, err := ExpandResources(tc.resources)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error calling ExpandResources: %s", err)
			}

			if len(output) != len(tc.want) {
				t.Fatalf("incorrect length of output, got %d, want %d", len(output), len(tc.want))
			}

			sortUnstructs(output)
			sortUnstructs(tc.want)

			for i := 0; i < len(output); i++ {
				diff := cmp.Diff(output[i], tc.want[i])
				if diff != "" {
					t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(output[i]), prettyResource(tc.want[i]), diff)
				}
			}
		})
	}
}

func sortUnstructs(unstructs []*unstructured.Unstructured) {
	sortKey := func(u *unstructured.Unstructured) string {
		return u.GetName() + u.GetKind() + u.GetResourceVersion() + u.GetAPIVersion()
	}
	sort.SliceStable(unstructs, func(x, y int) bool {
		return sortKey(unstructs[x]) < sortKey(unstructs[y])
	})
}
