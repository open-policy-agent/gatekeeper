package modifyset

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	spec = "spec"
)

func modifyset(value interface{}, location string) *unversioned.ModifySet {
	return &unversioned.ModifySet{
		Spec: unversioned.ModifySetSpec{
			ApplyTo: []match.MutationApplyTo{{
				ApplyTo: match.ApplyTo{
					Groups:   []string{"*"},
					Versions: []string{"*"},
					Kinds:    []string{"*"},
				},
			}},
			Location: location,
			Parameters: unversioned.ModifySetParameters{
				Operation: unversioned.MergeOp,
				Values: unversioned.Values{
					FromList: []interface{}{value},
				},
			},
		},
	}
}

func benchmarkModifySetMutator(b *testing.B, n int) {
	mutator, err := MutatorForModifySet(modifyset("foo", spec+strings.Repeat(".spec", n-1)))
	if err != nil {
		b.Fatal(err)
	}

	obj := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	p := make([]string, n)
	for i := 0; i < n; i++ {
		p[i] = spec
	}
	_, err = mutator.Mutate(&types.Mutable{Object: obj})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(&types.Mutable{Object: obj})
	}
}

func benchmarkNoModifySetMutator(b *testing.B, n int) {
	path := spec + strings.Repeat(".spec", n-1)
	a := modifyset("foo", path)
	a.Spec.Parameters.PathTests = []unversioned.PathTest{{
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
		p[i] = spec
	}
	_, err = mutator.Mutate(&types.Mutable{Object: obj})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(&types.Mutable{Object: obj})
	}
}

func BenchmarkModifySetSetterSetValueScale(b *testing.B) {
	for _, listSize := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("merge-existing/list-%d", listSize), func(b *testing.B) {
			obj := map[string]interface{}{"field": benchmarkModifySetList(listSize)}
			s := setter{values: []interface{}{"value-0"}, op: unversioned.MergeOp}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := s.SetValue(obj, "field"); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("merge-missing/list-%d", listSize), func(b *testing.B) {
			obj := map[string]interface{}{"field": benchmarkModifySetList(listSize)}
			s := setter{values: []interface{}{"missing-value"}, op: unversioned.MergeOp}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := s.SetValue(obj, "field"); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("prune-missing/list-%d", listSize), func(b *testing.B) {
			obj := map[string]interface{}{"field": benchmarkModifySetList(listSize)}
			s := setter{values: []interface{}{"missing-value"}, op: unversioned.PruneOp}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := s.SetValue(obj, "field"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkModifySetList(size int) []interface{} {
	values := make([]interface{}, size)
	for i := 0; i < size; i++ {
		values[i] = "value-" + fmt.Sprint(i)
	}
	return values
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
