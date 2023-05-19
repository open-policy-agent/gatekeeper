package expansion

import (
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	expansionunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
			generator: fixtures.LoadFixture(fixtures.GeneratorCat, t),
		},
		{
			name:      "generator with no gvk returns error",
			generator: fixtures.LoadFixture(fixtures.DeploymentNoGVK, t),
			expectErr: true,
		},
		{
			name:      "generator with non-matching template",
			generator: fixtures.LoadFixture(fixtures.GeneratorCat, t),
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{},
		},
		{
			name:      "no mutators basic deployment expands pod",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators:  []types.Mutator{},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodNoMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "deployment expands pod with enforcement action override",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators:  []types.Mutator{},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPodsEnforceDryrun, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodNoMutate, t), EnforcementAction: "dryrun", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "1 mutator basic deployment expands pod",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "expand with nil namespace returns error",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        nil,
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			expectErr: true,
		},
		{
			name:      "1 mutator source All deployment expands pod and mutates",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImageSourceAll, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "1 mutator source empty deployment expands pod and mutates",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImageSourceEmpty, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "1 mutator source Original deployment expands pod but does not mutate",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignHostnameSourceOriginal, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodNoMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "2 mutators, only 1 match, basic deployment expands pod",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImage, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotateKitten, t), // should not match
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodImagePullMutate, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "2 mutators, 2 matches, basic deployment expands pod",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignPullImage, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotatePod, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodImagePullMutateAnnotated, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "custom CR with 2 different resultant kinds, with mutators",
			generator: fixtures.LoadFixture(fixtures.GeneratorCat, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignKittenAge, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotatePurr, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotateKitten, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TemplateCatExpandsKitten, t),
				fixtures.LoadTemplate(fixtures.TemplateCatExpandsPurr, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.ResultantKitten, t), EnforcementAction: "dryrun", TemplateName: "expand-cats-kitten"},
				{Obj: fixtures.LoadFixture(fixtures.ResultantPurr, t), EnforcementAction: "warn", TemplateName: "expand-cats-purr"},
			},
		},
		{
			name:      "custom CR with 2 different resultant kinds, with mutators and non-matching configs",
			generator: fixtures.LoadFixture(fixtures.GeneratorCat, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssign(fixtures.AssignKittenAge, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotatePurr, t),
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotateKitten, t),
				fixtures.LoadAssign(fixtures.AssignPullImage, t), // should not match
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TemplateCatExpandsKitten, t),
				fixtures.LoadTemplate(fixtures.TemplateCatExpandsPurr, t),
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t), // should not match
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.ResultantKitten, t), EnforcementAction: "dryrun", TemplateName: "expand-cats-kitten"},
				{Obj: fixtures.LoadFixture(fixtures.ResultantPurr, t), EnforcementAction: "warn", TemplateName: "expand-cats-purr"},
			},
		},
		{
			name:      "1 mutator deployment expands pod with AssignImage",
			generator: fixtures.LoadFixture(fixtures.DeploymentNginx, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssignImage(fixtures.AssignImage, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpDeploymentExpandsPods, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.PodMutateImage, t), EnforcementAction: "", TemplateName: "expand-deployments"},
			},
		},
		{
			name:      "recursive expansion with mutators",
			generator: fixtures.LoadFixture(fixtures.GeneratorCronJob, t),
			ns:        &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			mutators: []types.Mutator{
				fixtures.LoadAssignMeta(fixtures.AssignMetaAnnotatePod, t),
			},
			templates: []*expansionunversioned.ExpansionTemplate{
				fixtures.LoadTemplate(fixtures.TempExpCronJob, t),
				fixtures.LoadTemplate(fixtures.TempExpJob, t),
			},
			want: []*Resultant{
				{Obj: fixtures.LoadFixture(fixtures.ResultantJob, t), EnforcementAction: "", TemplateName: "expand-cronjobs"},
				{Obj: fixtures.LoadFixture(fixtures.ResultantRecursivePod, t), EnforcementAction: "", TemplateName: "expand-jobs"},
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

	sortResultants(got)
	sortResultants(want)

	for i := 0; i < len(got); i++ {
		if diff := cmp.Diff(got[i], want[i]); diff != "" {
			t.Errorf("got = \n%s\n, want = \n%s\n\n diff:\n%s", prettyResource(got[i]), prettyResource(want[i]), diff)
		}
	}
}

func sortResultants(objs []*Resultant) {
	sortKey := func(r *Resultant) string {
		return r.Obj.GetName() + r.Obj.GetAPIVersion()
	}
	sort.Slice(objs, func(i, j int) bool {
		return sortKey(objs[i]) > sortKey(objs[j])
	})
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name  string
		errFn func(e error, t *testing.T)
		temp  expansionunversioned.ExpansionTemplate
	}{
		{
			name:  "valid expansion template",
			errFn: noError,
			temp:  *fixtures.TestTemplate("foo", 1, 2),
		},
		{
			name: "missing name",
			temp: *fixtures.NewTemplate(&fixtures.TemplateData{
				Apply: []match.ApplyTo{{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				}},
				Source: "spec.template",
				GenGVK: expansionunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}),
			errFn: matchErr("empty name"),
		},
		{
			name: "missing source",
			temp: *fixtures.NewTemplate(&fixtures.TemplateData{
				Name: "test1",
				Apply: []match.ApplyTo{{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				}},
				GenGVK: expansionunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}),
			errFn: matchErr("empty source"),
		},
		{
			name: "missing generated GVK",
			temp: *fixtures.NewTemplate(&fixtures.TemplateData{
				Name: "test1",
				Apply: []match.ApplyTo{{
					Groups:   []string{"apps"},
					Kinds:    []string{"Deployment"},
					Versions: []string{"v1"},
				}},
				Source: "spec.template",
			}),
			errFn: matchErr("empty generatedGVK"),
		},
		{
			name: "missing applyTo",
			temp: *fixtures.NewTemplate(&fixtures.TemplateData{
				Name:   "test1",
				Source: "spec.template",
				GenGVK: expansionunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}),
			errFn: matchErr("specify ApplyTo"),
		},
		{
			name: "loop",
			temp: *fixtures.NewTemplate(&fixtures.TemplateData{
				Name: "test1",
				Apply: []match.ApplyTo{{
					Groups:   []string{""},
					Kinds:    []string{"Pod"},
					Versions: []string{"v1"},
				}},
				Source: "spec.template",
				GenGVK: expansionunversioned.GeneratedGVK{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
			}),
			errFn: matchErr("also applies to that same GVK"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.errFn(ValidateTemplate(&tc.temp), t)
		})
	}
}

func noError(e error, t *testing.T) {
	if e != nil {
		t.Errorf("did want want error, but got %s", e)
	}
}

func matchErr(substr string) func(error, *testing.T) {
	return func(err error, t *testing.T) {
		if err == nil {
			t.Error("expected err but got nil")
			return
		}

		if !strings.Contains(err.Error(), substr) {
			t.Errorf("expected error to contain %q, but got %q", substr, err.Error())
		}
	}
}
