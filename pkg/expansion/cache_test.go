package expansion

import (
	"reflect"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type templateData struct {
	name         string
	apply        []match.ApplyTo
	source       string
	generatedGVK schema.GroupVersionKind
}

type assignData struct {
	name       string
	apply      []match.ApplyTo
	location   string
	match      match.Match
	parameters mutationsunversioned.Parameters
}

type assignMetadataData struct {
	name       string
	match      match.Match
	location   string
	parameters mutationsunversioned.MetadataParameters
}

type modifySetData struct {
	name       string
	match      match.Match
	location   string
	apply      []match.ApplyTo
	parameters mutationsunversioned.ModifySetParameters
}

func assignFromData(data *assignData) mutationsunversioned.Assign {
	return mutationsunversioned.Assign{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Assign",
			APIVersion: "mutations.gatekeeper.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: data.name},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo:    data.apply,
			Location:   data.location,
			Parameters: data.parameters,
			Match:      data.match,
		},
	}
}

func newAssign(data *assignData, t *testing.T) types.Mutator {
	a := assignFromData(data)
	mut, err := assign.MutatorForAssign(&a)
	if err != nil {
		t.Fatalf("error creating assign: %s\ndata: \n%+v\n", err, data)
		return nil
	}
	return mut
}

func assignMetadataFromData(data *assignMetadataData) mutationsunversioned.AssignMetadata {
	return mutationsunversioned.AssignMetadata{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AssignMetadata",
			APIVersion: "mutations.gatekeeper.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: data.name},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Match:      data.match,
			Location:   data.location,
			Parameters: data.parameters,
		},
	}
}

func newAssignMetadata(data *assignMetadataData, t *testing.T) types.Mutator {
	am := assignMetadataFromData(data)
	mut, err := assignmeta.MutatorForAssignMetadata(&am)
	if err != nil {
		t.Fatalf("error creating assignmetadata: %s\ndata:\n%+v\n", err, data)
		return nil
	}
	return mut
}

func modifySetFromData(data *modifySetData) mutationsunversioned.ModifySet {
	return mutationsunversioned.ModifySet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ModifySet",
			APIVersion: "mutations.gatekeeper.sh/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: data.name},
		Spec: mutationsunversioned.ModifySetSpec{
			ApplyTo:    data.apply,
			Match:      data.match,
			Location:   data.location,
			Parameters: data.parameters,
		},
	}
}

func newModifySet(data *modifySetData, t *testing.T) types.Mutator {
	ms := modifySetFromData(data)
	mut, err := modifyset.MutatorForModifySet(&ms)
	if err != nil {
		t.Fatalf("error creating modifyset: %s\ndata:\n%+v\n", err, data)
		return nil
	}
	return mut
}

func newTemplate(data *templateData) *mutationsunversioned.TemplateExpansion {
	return &mutationsunversioned.TemplateExpansion{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TemplateExpansion",
			APIVersion: "templateexpansions.gatekeeper.sh/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: data.name,
		},
		Spec: mutationsunversioned.TemplateExpansionSpec{
			ApplyTo:        data.apply,
			TemplateSource: data.source,
			GeneratedGVK:   data.generatedGVK,
		},
	}
}

func TestUpsertRemoveTemplate(t *testing.T) {
	tests := []struct {
		name          string
		add           []*mutationsunversioned.TemplateExpansion
		remove        []*mutationsunversioned.TemplateExpansion
		check         []*mutationsunversioned.TemplateExpansion
		wantAddErr    bool
		wantRemoveErr bool
	}{
		{
			name: "adding 2 valid templates",
			add: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			check: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			wantAddErr: false,
		},
		{
			name: "adding template with empty name returns error",
			add: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check:      []*mutationsunversioned.TemplateExpansion{},
			wantAddErr: true,
		},
		{
			name: "removing a template with empty name returns error",
			add: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			remove: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			wantAddErr:    false,
			wantRemoveErr: true,
		},
		{
			name: "adding 2 templates, removing 1",
			add: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			remove: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			wantAddErr:    false,
			wantRemoveErr: false,
		},
		{
			name: "updating an existing template",
			add: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"Baz"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v9000"},
					}},
					source: "spec.something",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v9000",
						Kind:    "Bar",
					},
				}),
			},
			check: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"Baz"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v9000"},
					}},
					source: "spec.something",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v9000",
						Kind:    "Bar",
					},
				}),
			},
			wantAddErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec, err := NewExpansionCache(nil, nil)
			if err != nil {
				t.Fatalf("failed to create cache: %s", err)
			}

			for _, templ := range tc.add {
				err := ec.UpsertTemplate(templ)
				if tc.wantAddErr && err == nil {
					t.Errorf("expected error, got nil")
				} else if !tc.wantAddErr && err != nil {
					t.Errorf("failed to add template: %s", err)
				}
			}

			for _, templ := range tc.remove {
				err := ec.RemoveTemplate(templ)
				if tc.wantRemoveErr && err == nil {
					t.Errorf("expected error, got nil")
				} else if !tc.wantRemoveErr && err != nil {
					t.Errorf("failed to remove template: %s", err)
				}
			}

			if len(ec.templates) != len(tc.check) {
				t.Errorf("incorrect number of templates in cache, got %d, want %d", len(ec.templates), len(tc.check))
			}
			for _, templ := range tc.check {
				k := templ.ObjectMeta.Name
				got, exists := ec.templates[k]
				if !exists {
					t.Errorf("could not find template with key %q", k)
				}
				if cmp.Diff(got, templ) != "" {
					t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", prettyResource(got), prettyResource(templ), cmp.Diff(got, templ))
				}
			}
		})
	}
}

func TestUpsertRemoveMutator(t *testing.T) {
	assignImagePullForPods := newAssign(&assignData{
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
	}, t)

	assignImagePullUpdated := newAssign(&assignData{
		name: "always-pull-image-pods",
		apply: []match.ApplyTo{{
			Groups:   []string{""},
			Kinds:    []string{"Pod"},
			Versions: []string{"v9000"},
		}},
		location: "spec.containers[name: *].imagePullPolicy",
		match: match.Match{
			Origin: "Generated",
			Scope:  "Namespaced",
		},
		parameters: mutationsunversioned.Parameters{
			Assign: mutationsunversioned.AssignField{
				Value: &types.Anything{Value: "Never"},
			},
		},
	}, t)

	assignMetadataAddAnnotation := newAssignMetadata(&assignMetadataData{
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
	}, t)

	modifySetRemoveErrLog := newModifySet(&modifySetData{
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
			Operation: mutationsunversioned.PruneOp,
			Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
		},
	}, t)

	tests := []struct {
		name          string
		add           []types.Mutator
		remove        []types.Mutator
		check         []types.Mutator
		wantAddErr    bool
		wantRemoveErr bool
	}{
		{
			name: "adding 3 valid mutators",
			add: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
			check: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
		},
		{
			name: "adding mutator without 'origin: Generated' returns error",
			add: []types.Mutator{newAssign(&assignData{
				name: "always-pull-image-pods",
				apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				location: "spec.containers[name: *].imagePullPolicy",
				match: match.Match{
					Scope: "Cluster",
				},
				parameters: mutationsunversioned.Parameters{
					Assign: mutationsunversioned.AssignField{
						Value: &types.Anything{Value: "Always"},
					},
				},
			}, t)},
			check:      []types.Mutator{},
			wantAddErr: true,
		},
		{
			name: "adding 3 mutators, removing 2",
			add: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
			remove: []types.Mutator{
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
			check: []types.Mutator{assignImagePullForPods.DeepCopy()},
		},
		{
			name: "updating an existing template",
			add: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignImagePullUpdated.DeepCopy(),
			},
			remove: []types.Mutator{
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
			check: []types.Mutator{assignImagePullUpdated.DeepCopy()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec, err := NewExpansionCache(nil, nil)
			if err != nil {
				t.Fatalf("failed to create cache: %s", err)
			}

			for _, mut := range tc.add {
				err := ec.UpsertMutator(mut)
				if tc.wantAddErr && err == nil {
					t.Errorf("expected error, got nil")
				} else if !tc.wantAddErr && err != nil {
					t.Errorf("failed to add mutator: %s", err)
				}
			}

			for _, mut := range tc.remove {
				err := ec.RemoveMutator(mut)
				if tc.wantRemoveErr && err == nil {
					t.Errorf("expected error, got nil")
				} else if !tc.wantRemoveErr && err != nil {
					t.Errorf("failed to remove mutator: %s", err)
				}
			}

			if len(ec.mutators) != len(tc.check) {
				t.Errorf("incorrect number of mutator in cache, got %d, want %d", len(ec.mutators), len(tc.check))
			}

			for _, mut := range tc.check {
				k := mut.ID()
				got, exists := ec.mutators[k]
				if !exists {
					t.Errorf("could not find mutator with key %q", k)
				}
				if !reflect.DeepEqual(got, mut) {
					t.Errorf("got mutator:\n%s\nwant mutator:\n%s", prettyResource(got), prettyResource(mut))
				}
			}
		})
	}
}

func TestMutatorsForGVK(t *testing.T) {
	assignImagePullForPods := newAssign(&assignData{
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
	}, t)

	assignDontMatchPod := newAssign(&assignData{
		name: "assign-dont-match-pods",
		apply: []match.ApplyTo{{
			Groups:   []string{""},
			Kinds:    []string{"Cat"},
			Versions: []string{"v9000"},
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
	}, t)

	assignMetadataAddAnnotation := newAssignMetadata(&assignMetadataData{
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
	}, t)

	modifySetRemoveErrLog := newModifySet(&modifySetData{
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
			Operation: mutationsunversioned.PruneOp,
			Values:    mutationsunversioned.Values{FromList: []interface{}{"--alsologtostderr"}},
		},
	}, t)

	tests := []struct {
		name     string
		gvk      schema.GroupVersionKind
		mutators []types.Mutator
		want     []types.Mutator
	}{
		{
			name: "assign mutator matches v1 pod",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{assignImagePullForPods.DeepCopy()},
			want:     []types.Mutator{assignImagePullForPods.DeepCopy()},
		},
		{
			name: "assign metadata mutator matches v1 pod",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{assignMetadataAddAnnotation.DeepCopy()},
			want:     []types.Mutator{assignMetadataAddAnnotation.DeepCopy()},
		},
		{
			name: "modify set matches v1 pod",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{modifySetRemoveErrLog.DeepCopy()},
			want:     []types.Mutator{modifySetRemoveErrLog.DeepCopy()},
		},
		{
			name: "3 mutators match v1 pod",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
			want: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignMetadataAddAnnotation.DeepCopy(),
				modifySetRemoveErrLog.DeepCopy(),
			},
		},
		{
			name: "no matching mutators",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{assignDontMatchPod.DeepCopy()},
			want:     []types.Mutator{},
		},
		{
			name: "no mutators, no matches",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{},
			want:     []types.Mutator{},
		},
		{
			name: "1 mutator matches, 1 mutator does not match",
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			mutators: []types.Mutator{
				assignImagePullForPods.DeepCopy(),
				assignDontMatchPod.DeepCopy(),
			},
			want: []types.Mutator{assignImagePullForPods.DeepCopy()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec, err := NewExpansionCache(tc.mutators, nil)
			if err != nil {
				t.Errorf("error creating expansion cache: %s", err)
			}

			got := ec.MutatorsForGVK(tc.gvk)
			if len(got) != len(tc.want) {
				t.Errorf("got %d mutators, want %d", len(got), len(tc.want))
			}

			sortMutators(got)
			sortMutators(tc.want)
			for i := 0; i < len(got); i++ {
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Errorf("got mutator:\n%s\nwant mutator:\n%s", prettyResource(got[i]), prettyResource(tc.want[i]))
				}
			}
		})
	}
}

func TestTemplatesForGVK(t *testing.T) {
	tests := []struct {
		name         string
		gvk          schema.GroupVersionKind
		addTemplates []*mutationsunversioned.TemplateExpansion
		want         []*mutationsunversioned.TemplateExpansion
	}{
		{
			name: "adding 2 templates, 1 match",
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			addTemplates: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			want: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
		},
		{
			name: "adding 2 templates, 2 matches",
			gvk: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			addTemplates: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			want: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
		},
		{
			name: "adding 1 templates, 0 match",
			addTemplates: []*mutationsunversioned.TemplateExpansion{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			want: []*mutationsunversioned.TemplateExpansion{},
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v9000",
				Kind:    "CronJob",
			},
		},
		{
			name:         "no templates, no matches",
			addTemplates: []*mutationsunversioned.TemplateExpansion{},
			want:         []*mutationsunversioned.TemplateExpansion{},
			gvk: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec, err := NewExpansionCache(nil, tc.addTemplates)
			if err != nil {
				t.Fatalf("failed to create cache: %s", err)
			}

			got := ec.TemplatesForGVK(tc.gvk)
			sortTemplates(got)
			wantSorted := make([]mutationsunversioned.TemplateExpansion, len(tc.want))
			for i := 0; i < len(tc.want); i++ {
				wantSorted[i] = *tc.want[i]
			}
			sortTemplates(wantSorted)

			if len(got) != len(wantSorted) {
				t.Errorf("want %d templates, got %d", len(wantSorted), len(got))
			}
			for i := 0; i < len(got); i++ {
				diff := cmp.Diff(got[i], wantSorted[i])
				if diff != "" {
					t.Errorf("got = \n%s\n, want = \n%s\n\n diff \n%s", prettyResource(got[i]), prettyResource(wantSorted[i]), diff)
				}
			}
		})
	}
}

func sortTemplates(templates []mutationsunversioned.TemplateExpansion) {
	sort.SliceStable(templates, func(x, y int) bool {
		return templates[x].Name < templates[y].Name
	})
}
