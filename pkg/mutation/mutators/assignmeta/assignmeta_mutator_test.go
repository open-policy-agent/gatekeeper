package assignmeta

import (
	"reflect"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/testhelpers"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
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

func newAssignMetadataMutator(t *testing.T, path string, value unversioned.AssignField) *Mutator {
	m := &unversioned.AssignMetadata{
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
		name     string
		obj      *unstructured.Unstructured
		path     string
		value    unversioned.AssignField
		expected interface{}
	}{
		{
			name:  "metadata value",
			path:  "metadata.labels.foo",
			value: unversioned.AssignField{FromMetadata: &unversioned.FromMetadata{Field: unversioned.ObjName}},
			obj:   newFoo(map[string]interface{}{}),
			expected: map[string]interface{}{
				"name": "my-foo",
				"labels": map[string]interface{}{
					"foo": "my-foo",
				},
			},
		},
		{
			name: "external data placeholder",
			path: "metadata.labels.foo",
			value: unversioned.AssignField{
				ExternalData: &unversioned.ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceUsername,
				},
			},
			obj: newFoo(map[string]interface{}{}),
			expected: map[string]interface{}{
				"name": "my-foo",
				"labels": map[string]interface{}{
					"foo": &unversioned.ExternalDataPlaceholder{
						Ref: &unversioned.ExternalData{
							Provider:   "some-provider",
							DataSource: types.DataSourceUsername,
						},
						ValueAtLocation: "kubernetes-admin",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			*externaldata.ExternalDataEnabled = true
			defer func() {
				*externaldata.ExternalDataEnabled = false
			}()

			mutator := newAssignMetadataMutator(t, test.path, test.value)
			obj := test.obj.DeepCopy()
			_, err := mutator.Mutate(&types.Mutable{Object: obj, Username: "kubernetes-admin"})
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}

			labels := obj.Object["metadata"]
			if !reflect.DeepEqual(labels, test.expected) {
				t.Errorf("metadata = %v; wanted %v", labels, test.expected)
			}
		})
	}
}

func Test_AssignMetadata_errors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mut    *unversioned.AssignMetadata
		errMsg string
	}{
		{
			name:   "empty spec",
			mut:    &unversioned.AssignMetadata{},
			errMsg: "invalid location for assignmetadata",
		},
		{
			name: "name > 63",
			mut: &unversioned.AssignMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name: testhelpers.BigName(),
				},
			},
			errMsg: core.ErrNameLength.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator, err := MutatorForAssignMetadata(tt.mut)

			require.ErrorContains(t, err, tt.errMsg)
			require.Nil(t, mutator)
		})
	}
}
