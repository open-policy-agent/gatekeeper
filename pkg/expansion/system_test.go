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
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignimage"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type templateData struct {
	name              string
	apply             []match.ApplyTo
	source            string
	generatedGVK      expansionunversioned.GeneratedGVK
	enforcementAction string
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
			ApplyTo:           data.apply,
			TemplateSource:    data.source,
			GeneratedGVK:      data.generatedGVK,
			EnforcementAction: data.enforcementAction,
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
		addErrIndex   int
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
			name: "adding template that creates cycle returns error",
			add: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "t1",
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
					name: "t2",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "MiniPod",
					},
				}),
				newTemplate(&templateData{
					name: "t3",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"MiniPod"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				}),
			},
			check: []*expansionunversioned.ExpansionTemplate{
				newTemplate(&templateData{
					name: "t1",
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
					name: "t2",
					apply: []match.ApplyTo{{
						Groups:   []string{""},
						Kinds:    []string{"Pod"},
						Versions: []string{"v1"},
					}},
					source: "spec.template",
					generatedGVK: expansionunversioned.GeneratedGVK{
						Group:   "",
						Version: "v1",
						Kind:    "MiniPod",
					},
				}),
			},
			wantAddErr:  true,
			addErrIndex: 2,
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

			for i, templ := range tc.add {
				err := ec.UpsertTemplate(templ)
				if i == tc.addErrIndex && tc.wantAddErr && err == nil {
					t.Errorf("expected error, got nil")
				} else if i == tc.addErrIndex && !tc.wantAddErr && err != nil {
					t.Errorf("failed to upsert template: %s", err)
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
				k := keyForTemplate(templ)
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

const (
	addOp  = "UPSERT"
	rmOp   = "REMOVE"
	mapKey = "OPS"
)

// mockGraphs mocks gvkGraph. Since gvkGraph is a type-casted map and all the
// functions are value receivers, the mock will stuff all the received
// operations in a map with a fixed key (mapKey).
type mockGraph map[string][]expansionOperation

type expansionOperation struct {
	op string
	t  *expansionunversioned.ExpansionTemplate
}

func (m mockGraph) addTemplate(t *expansionunversioned.ExpansionTemplate) error {
	m[mapKey] = append(m[mapKey], expansionOperation{
		op: addOp,
		t:  t,
	})
	return nil
}

func (m mockGraph) removeTemplate(t *expansionunversioned.ExpansionTemplate) {
	m[mapKey] = append(m[mapKey], expansionOperation{
		op: rmOp,
		t:  t,
	})
}

func TestGraph(t *testing.T) {
	tests := []struct {
		name    string
		runOps  []expansionOperation
		wantOps []expansionOperation
	}{
		{
			name: "adding template for first time adds to graph",
			runOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
			wantOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
		},
		{
			name: "adding template for second time removes and re-adds",
			runOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
			wantOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
		},
		{
			name: "add then remove same template",
			runOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
			wantOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
		},
		{
			name: "removing non-existing template does nothing",
			runOps: []expansionOperation{
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
			},
		},
		{
			name: "add 2 templates and remove 1 then update 1",
			runOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpCronJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpCronJob, t),
				},
			},
			wantOps: []expansionOperation{
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpCronJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpJob, t),
				},
				{
					op: rmOp,
					t:  loadTemplate(fixtures.TempExpCronJob, t),
				},
				{
					op: addOp,
					t:  loadTemplate(fixtures.TempExpCronJob, t),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expSystem := NewSystem(mutation.NewSystem(mutation.SystemOpts{}))
			mock := mockGraph{}
			expSystem.graph = mock

			for _, op := range tc.runOps {
				var err error
				switch op.op {
				case addOp:
					err = expSystem.UpsertTemplate(op.t)
				case rmOp:
					err = expSystem.RemoveTemplate(op.t)
				default:
					t.Fatalf("invalid op: %s", op.op)
				}
				if err != nil {
					t.Fatalf("unexpected error running op %s with template:\n%v", op.op, op.t)
				}
			}

			got := mock[mapKey]
			want := tc.wantOps
			if len(got) != len(want) {
				t.Errorf("got %d templates, but want %d", len(got), len(want))
			}
			for i := 0; i < len(got); i++ {
				gotOp := got[i]
				wantOp := want[i]
				if gotOp.op != wantOp.op {
					t.Errorf("got operation %s, but want %s", gotOp.op, wantOp.op)
				}
				if diff := cmp.Diff(gotOp.t, wantOp.t); diff != "" {
					t.Errorf("got template: %v\nbut want: %v\ndiff: %s", got[i], want[i], diff)
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
			expSystem := NewSystem(mutation.NewSystem(mutation.SystemOpts{}))
			for _, te := range tc.addTemplates {
				if err := expSystem.UpsertTemplate(te); err != nil {
					t.Fatalf("error upserting template: %s", err)
				}
			}

			got := expSystem.templatesForGVK(genGVKToSchemaGVK(tc.gvk))
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
		want      []*Resultant
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
			want: []*Resultant{},
		},
		{
			name:      "no mutators basic deployment expands pod",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators:  []types.Mutator{},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodNoMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "deployment expands pod with enforcement action override",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators:  []types.Mutator{},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPodsEnforceDryrun, t),
			},
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodNoMutate, t), EnforcementAction: "dryrun", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodNoMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodImagePullMutateAnnotated, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.ResultantKitten, t), EnforcementAction: "dryrun", TemplateName: "expand-cats-kitten"},
				{Obj: loadFixture(fixtures.ResultantPurr, t), EnforcementAction: "warn", TemplateName: "expand-cats-purr"},
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
			want: []*Resultant{
				{Obj: loadFixture(fixtures.ResultantKitten, t), EnforcementAction: "dryrun", TemplateName: "expand-cats-kitten"},
				{Obj: loadFixture(fixtures.ResultantPurr, t), EnforcementAction: "warn", TemplateName: "expand-cats-purr"},
			},
		},
		{
			name:      "1 mutator deployment expands pod with AssignImage",
			generator: loadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssignImage(fixtures.AssignImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: loadFixture(fixtures.PodMutateImage, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "recursive expansion with mutators",
			generator: loadFixture(fixtures.GeneratorCronJob, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				loadAssignMeta(fixtures.AssignMetaAnnotatePod, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				loadTemplate(fixtures.TempExpCronJob, t),
				loadTemplate(fixtures.TempExpJob, t),
			},
			want: []*Resultant{
				{Obj: loadFixture(fixtures.ResultantJob, t), EnforcementAction: "", TemplateName: "expand-cronjobs"},
				{Obj: loadFixture(fixtures.ResultantRecursivePod, t), EnforcementAction: "", TemplateName: "expand-jobs"},
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

func compareResults(got []*Resultant, want []*Resultant, t *testing.T) {
	if len(got) != len(want) {
		t.Errorf("got %d results, expected %d", len(got), len(want))
		return
	}

	sortReusultants(got)
	sortReusultants(want)

	for i := 0; i < len(got); i++ {
		if diff := cmp.Diff(got[i], want[i]); diff != "" {
			t.Errorf("got = \n%s\n, want = \n%s\n\n diff:\n%s", prettyResource(got[i]), prettyResource(want[i]), diff)
		}
	}
}

func sortReusultants(objs []*Resultant) {
	sortKey := func(r *Resultant) string {
		return r.Obj.GetName() + r.Obj.GetAPIVersion()
	}
	sort.Slice(objs, func(i, j int) bool {
		return sortKey(objs[i]) > sortKey(objs[j])
	})
}

func loadFixture(f string, t *testing.T) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(f), obj); err != nil {
		t.Fatalf("error unmarshaling yaml for fixture: %s\n%s", err, f)
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

func loadAssignImage(f string, t *testing.T) types.Mutator {
	u := loadFixture(f, t)
	a := &mutationsunversioned.AssignImage{}
	err := convertUnstructuredToTyped(u, a)
	if err != nil {
		t.Fatalf("error converting assignImage: %s", err)
	}
	mut, err := assignimage.MutatorForAssignImage(a)
	if err != nil {
		t.Fatalf("error creating assignimage: %s", err)
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
