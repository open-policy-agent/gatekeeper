package modifyset

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func makeValue(v interface{}) runtime.RawExtension {
	v2 := map[string]interface{}{
		"value": v,
	}
	j, err := json.Marshal(v2)
	if err != nil {
		panic(err)
	}
	return runtime.RawExtension{Raw: j}
}

func modifyset(value interface{}, location string) *v1alpha1.ModifySet {
	result := &v1alpha1.ModifySet{
		Spec: v1alpha1.ModifySetSpec{
			ApplyTo: []match.ApplyTo{{
				Groups:   []string{"*"},
				Versions: []string{"*"},
				Kinds:    []string{"*"},
			}},
			Location: location,
			Parameters: v1alpha1.ModifySetParameters{
				Values: v1alpha1.Values{
					FromList: []interface{}{makeValue(value)},
				},
			},
		},
	}

	return result
}

func benchmarkModifySetMutator(b *testing.B, n int) {
	mutator, err := MutatorForModifySet(modifyset("foo", "spec"+strings.Repeat(".spec", n-1)))
	if err != nil {
		b.Fatal(err)
	}

	obj := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	p := make([]string, n)
	for i := 0; i < n; i++ {
		p[i] = "spec"
	}
	_, err = mutator.Mutate(obj)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(obj)
	}
}

func benchmarkNoModifySetMutator(b *testing.B, n int) {
	path := "spec" + strings.Repeat(".spec", n-1)
	a := modifyset("foo", path)
	a.Spec.Parameters.PathTests = []v1alpha1.PathTest{{
		SubPath:   path,
		Condition: tester.MustNotExist,
	}}
	mutator, err := MutatorForModifySet(a)
	if err != nil {
		b.Fatal(err)
	}

	obj := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	p := make([]string, n)
	for i := 0; i < n; i++ {
		p[i] = "spec"
	}
	_, err = mutator.Mutate(obj)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(obj)
	}
}

func BenchmarkModifySetMutator_Mutate(b *testing.B) {
	ns := []int{1, 2, 5, 10, 20}

	for _, n := range ns {
		b.Run(fmt.Sprintf("always mutate %d-depth", n), func(b *testing.B) {
			benchmarkModifySetMutator(b, n)
		})
	}

	for _, n := range ns {
		b.Run(fmt.Sprintf("never mutate %d-depth", n), func(b *testing.B) {
			benchmarkNoModifySetMutator(b, n)
		})
	}
}
