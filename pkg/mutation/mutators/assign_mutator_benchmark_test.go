package mutators

import (
	"fmt"
	"strings"
	"testing"

	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func assign(value interface{}, location string) *v1alpha1.Assign {
	result := &v1alpha1.Assign{
		Spec: v1alpha1.AssignSpec{
			ApplyTo: []match.ApplyTo{{
				Groups:   []string{"*"},
				Versions: []string{"*"},
				Kinds:    []string{"*"},
			}},
			Location: location,
			Parameters: v1alpha1.Parameters{
				Assign: makeValue(value),
			},
		},
	}

	return result
}

func benchmarkAssignMutator(b *testing.B, n int) {
	providerCache := frameworksexternaldata.NewCache()
	mutator, err := MutatorForAssign(assign("foo", "spec"+strings.Repeat(".spec", n-1)), providerCache)
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
	providerResponseCache := make(map[types.ProviderCacheKey]string)
	_, err = mutator.Mutate(obj, providerResponseCache)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(obj, providerResponseCache)
	}
}

func benchmarkNoAssignMutator(b *testing.B, n int) {
	providerCache := frameworksexternaldata.NewCache()
	path := "spec" + strings.Repeat(".spec", n-1)
	a := assign("foo", path)
	a.Spec.Parameters.PathTests = []v1alpha1.PathTest{{
		SubPath:   path,
		Condition: tester.MustNotExist,
	}}
	mutator, err := MutatorForAssign(a, providerCache)
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
	providerResponseCache := make(map[types.ProviderCacheKey]string)
	_, err = mutator.Mutate(obj, providerResponseCache)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mutator.Mutate(obj, providerResponseCache)
	}
}

func BenchmarkAssignMutator_Mutate(b *testing.B) {
	ns := []int{1, 2, 5, 10, 20}

	for _, n := range ns {
		b.Run(fmt.Sprintf("always mutate %d-depth", n), func(b *testing.B) {
			benchmarkAssignMutator(b, n)
		})
	}

	for _, n := range ns {
		b.Run(fmt.Sprintf("never mutate %d-depth", n), func(b *testing.B) {
			benchmarkNoAssignMutator(b, n)
		})
	}
}
