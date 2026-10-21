package match

import (
	"testing"

	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func BenchmarkMatchesSelectors(b *testing.B) {
	mat := &Match{
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "gatekeeper"},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}
	target := benchmarkSelectorTarget()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matched, err := Matches(mat, target)
		if err != nil {
			b.Fatal(err)
		}
		if !matched {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkCompiledMatchesSelectors(b *testing.B) {
	mat := &Match{
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "gatekeeper"},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
	}
	compiled, err := Compile(mat)
	if err != nil {
		b.Fatal(err)
	}
	target := benchmarkSelectorTarget()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matched, err := compiled.Matches(target)
		if err != nil {
			b.Fatal(err)
		}
		if !matched {
			b.Fatal("expected match")
		}
	}
}

func benchmarkSelectorTarget() *Matchable {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("Pod")
	obj.SetNamespace("default")
	obj.SetLabels(map[string]string{"app": "gatekeeper"})
	return &Matchable{
		Object: obj,
		Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   "default",
			Labels: map[string]string{"env": "prod"},
		}},
		Source: mutationtypes.SourceTypeOriginal,
	}
}
