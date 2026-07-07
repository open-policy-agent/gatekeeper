package mutation

import (
	"context"
	"strconv"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
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
		_, _ = s.Mutate(context.Background(), &types.Mutable{Object: base, Operation: admissionv1.Create})
	}
}

func BenchmarkSystem_MutateNonMatchingMutatorScale(b *testing.B) {
	for _, mutatorCount := range []int{100, 1000} {
		b.Run(strconv.Itoa(mutatorCount), func(b *testing.B) {
			s := NewSystem(SystemOpts{})
			for i := 0; i < mutatorCount; i++ {
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
			}}
			base.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = s.Mutate(context.Background(), &types.Mutable{Object: base, Operation: admissionv1.Create})
			}
		})
	}
}

func BenchmarkSystem_MutateMatchingMutatorsLargeObject(b *testing.B) {
	for _, tc := range []struct {
		name         string
		mutatorCount int
		dataEntries  int
		preMutated   bool
	}{
		{name: "matching-1/data-500/fresh", mutatorCount: 1, dataEntries: 500},
		{name: "matching-10/data-500/fresh", mutatorCount: 10, dataEntries: 500},
		{name: "matching-100/data-500/fresh", mutatorCount: 100, dataEntries: 500},
		{name: "matching-1000/data-500/fresh", mutatorCount: 1000, dataEntries: 500},
		{name: "matching-1/data-500/already-mutated", mutatorCount: 1, dataEntries: 500, preMutated: true},
		{name: "matching-10/data-500/already-mutated", mutatorCount: 10, dataEntries: 500, preMutated: true},
		{name: "matching-100/data-500/already-mutated", mutatorCount: 100, dataEntries: 500, preMutated: true},
		{name: "matching-1000/data-500/already-mutated", mutatorCount: 1000, dataEntries: 500, preMutated: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := NewSystem(SystemOpts{})
			for i := 0; i < tc.mutatorCount; i++ {
				a := assign("matched"+strconv.Itoa(i), "spec.field"+strconv.Itoa(i))
				a.Name = "assign-matching-" + strconv.Itoa(i)
				a.Spec.ApplyTo = []match.MutationApplyTo{{
					ApplyTo: match.ApplyTo{
						Groups:   []string{""},
						Versions: []string{"v1"},
						Kinds:    []string{"ConfigMap"},
					},
				}}
				mutator, err := mutators.MutatorForAssign(a)
				if err != nil {
					b.Fatal(err)
				}
				if err := s.Upsert(mutator); err != nil {
					b.Fatal(err)
				}
			}

			base := benchmarkMutationObject(tc.dataEntries, tc.preMutated, tc.mutatorCount)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u := base
				if !tc.preMutated {
					b.StopTimer()
					u = base.DeepCopy()
					b.StartTimer()
				}
				if _, err := s.Mutate(context.Background(), &types.Mutable{Object: u, Operation: admissionv1.Create}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSystem_MutateSkipsPlaceholderScanWithoutTerminatingMutators(b *testing.B) {
	for _, dataEntries := range []int{500, 5000} {
		b.Run("data-"+strconv.Itoa(dataEntries), func(b *testing.B) {
			s := NewSystem(SystemOpts{})
			a := assign("matched", "spec.matched")
			a.Name = "assign-matching"
			a.Spec.ApplyTo = []match.MutationApplyTo{{
				ApplyTo: match.ApplyTo{
					Groups:   []string{""},
					Versions: []string{"v1"},
					Kinds:    []string{"ConfigMap"},
				},
			}}
			mutator, err := mutators.MutatorForAssign(a)
			if err != nil {
				b.Fatal(err)
			}
			if err := s.Upsert(mutator); err != nil {
				b.Fatal(err)
			}

			base := benchmarkMutationObject(dataEntries, false, 1)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				u := base.DeepCopy()
				b.StartTimer()
				if _, err := s.Mutate(context.Background(), &types.Mutable{Object: u, Operation: admissionv1.Create}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkMutationObject(dataEntries int, preMutated bool, mutatorCount int) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm",
			"namespace": "default",
		},
		"data": benchmarkLargeObjectData(dataEntries),
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

	if preMutated {
		spec := make(map[string]interface{}, mutatorCount)
		for i := 0; i < mutatorCount; i++ {
			spec["field"+strconv.Itoa(i)] = "matched" + strconv.Itoa(i)
		}
		obj.Object["spec"] = spec
	}

	return obj
}

func benchmarkLargeObjectData(entries int) map[string]interface{} {
	data := make(map[string]interface{}, entries)
	for i := 0; i < entries; i++ {
		data["key"+strconv.Itoa(i)] = "value" + strconv.Itoa(i)
	}
	return data
}

func BenchmarkSystem_MutateCandidateIndexScale(b *testing.B) {
	for _, tc := range []struct {
		name        string
		nonmatching int
	}{
		{name: "matching-1/nonmatching-0", nonmatching: 0},
		{name: "matching-1/nonmatching-100", nonmatching: 100},
		{name: "matching-1/nonmatching-1000", nonmatching: 1000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := NewSystem(SystemOpts{})

			matching := assign("matched", "spec.matched")
			matching.Name = "assign-matching"
			matching.Spec.ApplyTo = []match.MutationApplyTo{{
				ApplyTo: match.ApplyTo{
					Groups:   []string{""},
					Versions: []string{"v1"},
					Kinds:    []string{"ConfigMap"},
				},
			}}
			matchingMutator, err := mutators.MutatorForAssign(matching)
			if err != nil {
				b.Fatal(err)
			}
			if err := s.Upsert(matchingMutator); err != nil {
				b.Fatal(err)
			}

			for i := 0; i < tc.nonmatching; i++ {
				nonmatching := assign("", "spec.nonmatching"+strconv.Itoa(i))
				nonmatching.Name = "assign-nonmatching-" + strconv.Itoa(i)
				nonmatching.Spec.ApplyTo = []match.MutationApplyTo{{
					ApplyTo: match.ApplyTo{
						Groups:   []string{"apps"},
						Versions: []string{"v1"},
						Kinds:    []string{"Deployment"},
					},
				}}
				mutator, err := mutators.MutatorForAssign(nonmatching)
				if err != nil {
					b.Fatal(err)
				}
				if err := s.Upsert(mutator); err != nil {
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
			}}
			base.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.Mutate(context.Background(), &types.Mutable{Object: base, Operation: admissionv1.Create}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
