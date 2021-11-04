package assignmeta

import (
	"testing"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newFoo(spec map[string]interface{}) *unstructured.Unstructured {
	data := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Foo",
		"metadata": map[string]interface{}{
			"name": "my-foo",
		},
	}
	if spec != nil {
		data["spec"] = spec
	}
	return &unstructured.Unstructured{Object: data}
}

func newAssignMetadataMutator(t *testing.T, path string, value mutationsunversioned.AssignField) *Mutator {
	m := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name: "Foo",
		},
	}
	m.Spec.Parameters.Assign = value
	m.Spec.Location = path

	m2, err := MutatorForAssignMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	return m2
}

func TestAssignMetadata(t *testing.T) {
	tests := []struct {
		name  string
		obj   *unstructured.Unstructured
		path  string
		value mutationsunversioned.AssignField
	}{
		{
			name:  "metadata value",
			path:  "metadata.labels.foo",
			value: mutationsunversioned.AssignField{FromMetadata: &mutationsunversioned.FromMetadata{Field: mutationsunversioned.ObjName}},
			obj:   newFoo(map[string]interface{}{}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAssignMetadataMutator(t, test.path, test.value)
			obj := test.obj.DeepCopy()
			_, err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			labels := obj.GetLabels()
			if labels["foo"] != "my-foo" {
				t.Errorf("metadata.labels.foo = %v; wanted %v", labels["foo"], "my-foo")
			}
		})
	}
}
