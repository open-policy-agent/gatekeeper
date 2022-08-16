package expansion

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion/fixtures"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type templateData struct {
	name         string
	apply        []match.ApplyTo
	source       string
	generatedGVK expansionunversioned.GeneratedGVK
}

func newTemplate(data *templateData) *expansionunversioned.ExpansionTemplate {
	return &expansionunversioned.ExpansionTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExpansionTemplate",
			APIVersion: "expansiontemplates.gatekeeper.sh/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: data.name,
		},
		Spec: expansionunversioned.ExpansionTemplateSpec{
			ApplyTo:        data.apply,
			TemplateSource: data.source,
			GeneratedGVK:   data.generatedGVK,
		},
	}
}

func TestUpsertRemoveTemplate(t *testing.T) {
	tests := []struct {
		name          string
		add           []*expansionunversioned.ExpansionTemplate
		remove        []*expansionunversioned.ExpansionTemplate
		check         []*expansionunversioned.ExpansionTemplate
		wantAddErr    bool
		wantRemoveErr bool
	}{
		{
			name: "adding 2 valid templates",
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			check: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
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
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check:      []*expansionunversioned.ExpansionTemplate{},
			wantAddErr: true,
		},
		{
			name: "adding template with empty source returns error",
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "hello",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check:      []*expansionunversioned.ExpansionTemplate{},
			wantAddErr: true,
		},
		{
			name: "adding template with empty GVK returns error",
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "hello",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source:       "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{},
				}),
			},
			check:      []*expansionunversioned.ExpansionTemplate{},
			wantAddErr: true,
		},
		{
			name: "removing a template with empty name returns error",
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			remove: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			remove: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test2",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			check: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v9000",
						Kind:    "Bar",
					},
				}),
			},
			check: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"Baz"},
						Kinds:    []string{"Foo"},
						Versions: []string{"v9000"},
					}},
					source: "spec.something",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
			ec := NewSystem(mutation.NewSystem(mutation.SystemOpts{}))

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

func TestTemplatesForGVK(t *testing.T) {
	tests := []struct {
		name         string
		gvk          expansionunversioned.GeneratedGVK
		addTemplates []*expansionunversioned.ExpansionTemplate
		want         []*expansionunversioned.ExpansionTemplate
	}{
		{
			name: "adding 2 templates, 1 match",
			gvk: expansionunversioned.GeneratedGVK{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			addTemplates: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			want: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
		},
		{
			name: "adding 2 templates, 2 matches",
			gvk: expansionunversioned.GeneratedGVK{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			addTemplates: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
			want: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
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
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Bar",
					},
				}),
			},
		},
		{
			name: "adding 1 templates, 0 match",
			addTemplates: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "test1",
					apply: []match.ApplyTo{{
						Groups:   []string{"apps"},
						Kinds:    []string{"Deployment"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			want: []*expansionunversioned.ExpansionTemplate{},
			gvk: expansionunversioned.GeneratedGVK{
				Group:   "",
				Version: "v9000",
				Kind:    "CronJob",
			},
		},
		{
			name:         "no templates, no matches",
			addTemplates: []*expansionunversioned.ExpansionTemplate{},
			want:         []*expansionunversioned.ExpansionTemplate{},
			gvk: expansionunversioned.GeneratedGVK{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec := NewSystem(mutation.NewSystem(mutation.SystemOpts{}))
			for _, te := range tc.addTemplates {
				if err := ec.UpsertTemplate(te); err != nil {
					t.Fatalf("error upserting template: %s", err)
				}
			}

			got := ec.templatesForGVK(genGVKToSchemaGVK(tc.gvk))
			sortTemplates(got)
			wantSorted := make([]*expansionunversioned.ExpansionTemplate, len(tc.want))
			copy(wantSorted, tc.want)
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

func TestExpand(t *testing.T) {
	tests := []struct {
		name      string
		generator *unstructured.Unstructured
		ns        *corev1.Namespace
		templates []*expansionunversioned.ExpansionTemplate
		mutators  []types.Mutator
		want      []*unstructured.Unstructured
		expectErr bool
	}{
		{
			name:      "generator with no templates or mutators",
			generator: loadFixture(fixtures.GeneratorCat, t),
		},
		{
			name:      "generator with no gvk returns error",
			generator: loadFixture(fixtures.DeploymentNoGVK, t),
			expectErr: true,
		},
		{
			name:      "generator with non-matching template",
			generator: loadFixture(fixtures.GeneratorCat, t),
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{},
		},
		{
			name:      "no mutators basic deployment expands pod",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators:  []types.Mutator{},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodNoMutate, t)},
		},
		{
			name:      "1 mutator basic deployment expands pod",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodImagePullMutate, t)},
		},
		{
			name:      "expand with nil namespace returns error",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        nil,
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			expectErr: true,
		},
		{
			name:      "1 mutator source All deployment expands pod and mutates",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImageSourceAll, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodImagePullMutate, t)},
		},
		{
			name:      "1 mutator source empty deployment expands pod and mutates",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImageSourceEmpty, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodImagePullMutate, t)},
		},
		{
			name:      "1 mutator source Original deployment expands pod but does not mutate",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignHostnameSourceOriginal, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodNoMutate, t)},
		},
		{
			name:      "2 mutators, only 1 match, basic deployment expands pod",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImage, t),
				loadAssignMeta(fixtures.AssignMetaAnnotateKitten, t), // should not match
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodImagePullMutate, t)},
		},
		{
			name:      "2 mutators, 2 matches, basic deployment expands pod",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignPullImage, t),
				loadAssignMeta(fixtures.AssignMetaAnnotatePod, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*unstructured.Unstructured{loadFixture(fixtures.PodImagePullMutateAnnotated, t)},
		},
		{
			name:      "custom CR with 2 different resultant kinds, with mutators",
			generator: loadFixture(fixtures.GeneratorCat, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignKittenAge, t),
				loadAssignMeta(fixtures.AssignMetaAnnotatePurr, t),
				loadAssignMeta(fixtures.AssignMetaAnnotateKitten, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TemplateCatExpandsKitten, t),
				loadTemplate(fixtures.TemplateCatExpandsPurr, t),
			},
			want: []*unstructured.Unstructured{
				loadFixture(fixtures.ResultantKitten, t),
				loadFixture(fixtures.ResultantPurr, t),
			},
		},
		{
			name:      "custom CR with 2 different resultant kinds, with mutators and non-matching configs",
			generator: loadFixture(fixtures.GeneratorCat, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssign(fixtures.AssignKittenAge, t),
				loadAssignMeta(fixtures.AssignMetaAnnotatePurr, t),
				loadAssignMeta(fixtures.AssignMetaAnnotateKitten, t),
				loadAssign(fixtures.AssignPullImage, t), // should not match
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TemplateCatExpandsKitten, t),
				loadTemplate(fixtures.TemplateCatExpandsPurr, t),
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t), // should not match
			},
			want: []*unstructured.Unstructured{
				loadFixture(fixtures.ResultantKitten, t),
				loadFixture(fixtures.ResultantPurr, t),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutSystem := mutation.NewSystem(mutation.SystemOpts{})
			for _, m := range tc.mutators {
				if err := mutSystem.Upsert(m); err != nil {
					t.Fatalf("error upserting mutator: %s", err)
				}
			}

			expSystem := NewSystem(mutSystem)
			for _, te := range tc.templates {
				if err := expSystem.UpsertTemplate(te); err != nil {
					t.Fatalf("error upserting template: %s", err)
				}
			}

			base := &types.Mutable{
				Object:    tc.generator,
				Namespace: tc.ns,
				Username:  "unit-test",
				Source:    types.SourceTypeGenerated,
			}
			results, err := expSystem.Expand(base)
			if tc.expectErr && err == nil {
				t.Errorf("want error, got nil")
			} else if !tc.expectErr && err != nil {
				t.Errorf("unexpected err: %s", err)
			}

			compareResults(results, tc.want, t)
		})
	}
}

func compareResults(got []*unstructured.Unstructured, want []*unstructured.Unstructured, t *testing.T) {
	if len(got) != len(want) {
		t.Errorf("got %d results, expected %d", len(got), len(want))
		return
	}

	sortUnstructs(got)
	sortUnstructs(want)

	for i := 0; i < len(got); i++ {
		if diff := cmp.Diff(got[i], want[i]); diff != "" {
			t.Errorf("got = \n%s\n, want = \n%s\n\n diff:\n%s", prettyResource(got[i]), prettyResource(want[i]), diff)
		}
	}
}

func sortUnstructs(objs []*unstructured.Unstructured) {
	sortKey := func(o *unstructured.Unstructured) string {
		return o.GetName() + o.GetAPIVersion()
	}
	sort.Slice(objs, func(i, j int) bool {
		return sortKey(objs[i]) > sortKey(objs[j])
	})
}

func loadFixture(f string, t *testing.T) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(f), obj); err != nil {
		t.Fatalf("error unmarshaling yaml for fixture: %s", err)
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("error marshaling json for fixture: %s", err)
	}

	if err = json.Unmarshal(jsonBytes, &obj); err != nil {
		t.Fatalf("error unmarshaling json for fixture: %s", err)
	}

	u := unstructured.Unstructured{}
	u.SetUnstructuredContent(obj)
	return &u
}

func loadTemplate(f string, t *testing.T) *expansionunversioned.ExpansionTemplate {
	u := loadFixture(f, t)
	te := &expansionunversioned.ExpansionTemplate{}
	err := convertUnstructuredToTyped(u, te)
	if err != nil {
		t.Fatalf("error converting template expansion: %s", err)
	}
	return te
}

func loadAssign(f string, t *testing.T) types.Mutator {
	u := loadFixture(f, t)
	a := &mutationsunversioned.Assign{}
	err := convertUnstructuredToTyped(u, a)
	if err != nil {
		t.Fatalf("error converting assign: %s", err)
	}
	mut, err := assign.MutatorForAssign(a)
	if err != nil {
		t.Fatalf("error creating assign: %s", err)
	}
	return mut
}

func loadAssignMeta(f string, t *testing.T) types.Mutator {
	u := loadFixture(f, t)
	a := &mutationsunversioned.AssignMetadata{}
	err := convertUnstructuredToTyped(u, a)
	if err != nil {
		t.Fatalf("error converting assignmeta: %s", err)
	}
	mut, err := assignmeta.MutatorForAssignMetadata(a)
	if err != nil {
		t.Fatalf("error creating assignmeta: %s", err)
	}
	return mut
}

func convertUnstructuredToTyped(u *unstructured.Unstructured, obj interface{}) error {
	if u == nil {
		return fmt.Errorf("cannot convert nil unstructured to type")
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), obj)
	return err
}

func sortTemplates(templates []*expansionunversioned.ExpansionTemplate) {
	sort.SliceStable(templates, func(x, y int) bool {
		return templates[x].Name < templates[y].Name
	})
}
