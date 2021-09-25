package assignmeta

import (
	"fmt"
	"testing"

	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
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

func newAssignMetadataMutator(path string, value mutationsv1alpha1.AssignField) *Mutator {
	m := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name: "Foo",
		},
	}
	m.Spec.Parameters.Assign = value
	m.Spec.Location = path

	m2, err := MutatorForAssignMetadata(m)
	if err != nil {
		panic(err)
	}
	return m2
}

func TestAssign(t *testing.T) {
	tests := []struct {
		name  string
		obj   *unstructured.Unstructured
		path  string
		value mutationsv1alpha1.AssignField
		fn    func(*unstructured.Unstructured) error
	}{
		{
			name:  "metadata value",
			path:  "metadata.labels.foo",
			value: mutationsv1alpha1.AssignField{FromMetadata: &mutationsv1alpha1.FromMetadata{Field: mutationsv1alpha1.ObjName}},
			obj:   newFoo(map[string]interface{}{}),
			fn: func(u *unstructured.Unstructured) error {
				labels := u.GetLabels()
				if labels["foo"] != "my-foo" {
					return fmt.Errorf("metadata.labels.foo = %v; wanted %v", labels["foo"], "my-foo")
				}
				return nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAssignMetadataMutator(test.path, test.value)
			obj := test.obj.DeepCopy()
			_, err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			if err := test.fn(obj); err != nil {
				t.Errorf("failed test: %v", err)
			}
		})
	}
}
