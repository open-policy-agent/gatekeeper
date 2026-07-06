package assignimage

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

func assignImage(domain, path, tag, location string) *unversioned.AssignImage {
	result := &unversioned.AssignImage{
		Spec: unversioned.AssignImageSpec{
			ApplyTo: []match.MutationApplyTo{{
				ApplyTo: match.ApplyTo{
					Groups:   []string{"*"},
					Versions: []string{"*"},
					Kinds:    []string{"*"},
				},
			}},
			Location: location,
			Parameters: unversioned.AssignImageParameters{
				AssignDomain: domain,
				AssignPath:   path,
				AssignTag:    tag,
			},
		},
	}

	return result
}

func benchmarkAssignImageMutator(b *testing.B, n int) {
	ai := assignImage("a.b.c", "lib/repo", ":latest", spec+strings.Repeat(".spec", n-1))
	mutator, err := MutatorForAssignImage(ai)
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

func benchmarkNoAssignImageMutator(b *testing.B, n int) {
	location := spec + strings.Repeat(".spec", n-1)
	a := assignImage("a.b.c", "lib/repo", ":latest", location)
	a.Spec.Parameters.PathTests = []unversioned.PathTest{{
		SubPath:   location,
		Condition: tester.MustNotExist,
	}}
	mutator, err := MutatorForAssignImage(a)
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

func BenchmarkAssignImageMutator_Mutate(b *testing.B) {
	ns := []int{1, 2, 5, 10, 20}

	for _, n := range ns {
		b.Run(fmt.Sprintf("always mutate %d-depth", n), func(b *testing.B) {
			benchmarkAssignImageMutator(b, n)
		})
	}

	for _, n := range ns {
		b.Run(fmt.Sprintf("never mutate %d-depth", n), func(b *testing.B) {
			benchmarkNoAssignImageMutator(b, n)
		})
	}
}
