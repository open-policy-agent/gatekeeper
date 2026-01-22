package fixtures

import (
	"encoding/json"
	"fmt"
	"testing"

	expansionunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignimage"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"go.yaml.in/yaml/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type TemplateData struct {
	Name              string
	Apply             []match.ApplyTo
	Source            string
	GenGVK            expansionunversioned.GeneratedGVK
	EnforcementAction string
}

func NewTemplate(data *TemplateData) *expansionunversioned.ExpansionTemplate {
	return &expansionunversioned.ExpansionTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExpansionTemplate",
			APIVersion: "expansiontemplates.gatekeeper.sh/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: data.Name,
		},
		Spec: expansionunversioned.ExpansionTemplateSpec{
			ApplyTo:           data.Apply,
			TemplateSource:    data.Source,
			GeneratedGVK:      data.GenGVK,
			EnforcementAction: data.EnforcementAction,
		},
	}
}

func LoadFixture(f string, t *testing.T) *unstructured.Unstructured {
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

func LoadTemplate(f string, t *testing.T) *expansionunversioned.ExpansionTemplate {
	u := LoadFixture(f, t)
	te := &expansionunversioned.ExpansionTemplate{}
	err := convertUnstructuredToTyped(u, te)
	if err != nil {
		t.Fatalf("error converting template expansion: %s", err)
	}
	return te
}

func LoadAssign(f string, t *testing.T) types.Mutator {
	u := LoadFixture(f, t)
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

func LoadAssignImage(f string, t *testing.T) types.Mutator {
	u := LoadFixture(f, t)
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

func LoadAssignMeta(f string, t *testing.T) types.Mutator {
	u := LoadFixture(f, t)
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

func TestTemplate(name string, applyID, genID int) *expansionunversioned.ExpansionTemplate {
	return NewTemplate(&TemplateData{
		Name: name,
		Apply: []match.ApplyTo{{
			Groups:   []string{fmt.Sprintf("group%d", applyID)},
			Versions: []string{fmt.Sprintf("v%d", applyID)},
			Kinds:    []string{fmt.Sprintf("kind%d", applyID)},
		}},
		Source: "spec.template",
		GenGVK: expansionunversioned.GeneratedGVK{
			Group:   fmt.Sprintf("group%d", genID),
			Version: fmt.Sprintf("v%d", genID),
			Kind:    fmt.Sprintf("kind%d", genID),
		},
	})
}

func TempMultApply() *expansionunversioned.ExpansionTemplate {
	return NewTemplate(&TemplateData{
		Name: "t2",
		Apply: []match.ApplyTo{
			{
				Groups:   []string{"group1"},
				Versions: []string{"v1"},
				Kinds:    []string{"kind1"},
			},
			{
				Groups:   []string{"group11"},
				Versions: []string{"v11", "v22"},
				Kinds:    []string{"kind11"},
			},
		},
		Source: "spec.template",
		GenGVK: expansionunversioned.GeneratedGVK{
			Group:   "group2",
			Version: "v2",
			Kind:    "kind2",
		},
	})
}
