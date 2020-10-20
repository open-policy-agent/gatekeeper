package mutation_test

import (
	"testing"

	configv1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	mutationsv1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var mutationScheme = runtime.NewScheme()
var mutationCache *mutation.Cache

func init() {
	configv1.AddToScheme(mutationScheme)
	mutationCache = mutation.NewCache(mutationScheme)
}

func TestMetadataMutation(t *testing.T) {
	mutation1 := &mutationsv1.AssignMetadata{
		Spec: mutationsv1.AssignMetadataSpec{
			Match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{"", "Pod"},
				},
				Namespaces: []string{"foo"},
			},
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				"fooann": "barann",
			},
		},
	}
	mutationCache.Insert(mutation.MetadataMutator{AssignMetadata: mutation1})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "foo",
		},
	}
	gvk := metav1.GroupVersionKind{
		Group: "",
		Kind:  "Pod",
	}
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		t.Errorf("Failed to convert pod to unstructured %v", err)
	}

	mutated, err := mutation.ApplyMutations(mutationCache, unstructured.Unstructured{Object: unstructuredPod}, gvk, nil)
	if err != nil {
		t.Errorf("Failed to apply mutation to pod %v", err)
	}
	mutatedMeta, err := meta.Accessor(mutated)

	if mutatedMeta.GetLabels()["foo"] != "bar" {
		t.Errorf("Label not added")
	}

	if mutatedMeta.GetAnnotations()["fooann"] != "barann" {
		t.Errorf("Annotation not added")
	}
}
