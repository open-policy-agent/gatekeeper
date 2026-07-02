package mutation

import (
	"context"
	"strconv"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func makeValue(v interface{}) unversioned.AssignField {
	return unversioned.AssignField{Value: &types.Anything{Value: v}}
}

func assign(value interface{}, location string) *unversioned.Assign {
	result := &unversioned.Assign{
		Spec: unversioned.AssignSpec{
			ApplyTo: []match.MutationApplyTo{{
				ApplyTo: match.ApplyTo{
					Groups:   []string{"*"},
					Versions: []string{"*"},
					Kinds:    []string{"*"},
				},
			}},
			Location: location,
			Parameters: unversioned.Parameters{
				Assign: makeValue(value),
			},
		},
	}

	return result
}

func BenchmarkSystem_Mutate(b *testing.B) {
	s := NewSystem(SystemOpts{})

	a := assign("", "spec")
	m, err := mutators.MutatorForAssign(a)
	if err != nil {
		b.Fatal(err)
	}

	err = s.Upsert(m)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < b.N; i++ {
		u := &unstructured.Unstructured{}

		_, _ = s.Mutate(context.Background(), &types.Mutable{Object: u})
	}
}

func BenchmarkSystem_MutateNonMatchingMutatorsLargeObject(b *testing.B) {
	s := NewSystem(SystemOpts{})
	for i := 0; i < 100; i++ {
		a := assign("", "spec.field"+strconv.Itoa(i))
		a.Name = "assign-" + strconv.Itoa(i)
		a.Spec.ApplyTo = []match.MutationApplyTo{{
			ApplyTo: match.ApplyTo{
				Groups:   []string{"apps"},
				Versions: []string{"v1"},
				Kinds:    []string{"Deployment"},
			},
		}}
		m, err := mutators.MutatorForAssign(a)
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Upsert(m); err != nil {
			b.Fatal(err)
		}
	}

	base := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm",
			"namespace": "default",
		},
		"data": benchmarkLargeObjectData(500),
	}}
	base.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Mutate(context.Background(), &types.Mutable{Object: base})
	}
}

func benchmarkLargeObjectData(entries int) map[string]interface{} {
	data := make(map[string]interface{}, entries)
	for i := 0; i < entries; i++ {
		data["key"+strconv.Itoa(i)] = "value" + strconv.Itoa(i)
	}
	return data
}
