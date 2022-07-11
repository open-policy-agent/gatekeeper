package expansion

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type templateData struct {
	name         string
	apply        []match.ApplyTo
	source       string
	generatedGVK mutationsunversioned.GeneratedGVK
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
			ec := NewSystem()

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
		gvk          mutationsunversioned.GeneratedGVK
		addTemplates []*mutationsunversioned.TemplateExpansion
		want         []*mutationsunversioned.TemplateExpansion
	}{
		{
			name: "adding 2 templates, 1 match",
			gvk: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
		},
		{
			name: "adding 2 templates, 2 matches",
			gvk: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
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
					generatedGVK: mutationsunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}),
			},
			want: []*mutationsunversioned.TemplateExpansion{},
			gvk: mutationsunversioned.GeneratedGVK{
				Group:   "",
				Version: "v9000",
				Kind:    "CronJob",
			},
		},
		{
			name:         "no templates, no matches",
			addTemplates: []*mutationsunversioned.TemplateExpansion{},
			want:         []*mutationsunversioned.TemplateExpansion{},
			gvk: mutationsunversioned.GeneratedGVK{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec := NewSystem()

			got := ec.TemplatesForGVK(genGVKToSchemaGVK(tc.gvk))
			sortTemplates(got)
			wantSorted := make([]*mutationsunversioned.TemplateExpansion, len(tc.want))
			for i := 0; i < len(tc.want); i++ {
				wantSorted[i] = tc.want[i]
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

func sortTemplates(templates []*mutationsunversioned.TemplateExpansion) {
	sort.SliceStable(templates, func(x, y int) bool {
		return templates[x].Name < templates[y].Name
	})
}
