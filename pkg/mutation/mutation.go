package mutation

import (
	"encoding/json"

	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Mutator is a wrapper to runtime object that describe mutations.
type Mutator interface {
	Mutate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	Matches(obj *unstructured.Unstructured, gvk metav1.GroupVersionKind, ns *corev1.Namespace) (bool, error)
	Obj() runtime.Object
}

// ApplyMutations applies the mutations contained in the cache to the given object.
// The gvk is passed explicitly because
func ApplyMutations(cache *Cache, mutating unstructured.Unstructured, gvk metav1.GroupVersionKind, ns *corev1.Namespace) (*unstructured.Unstructured, error) {
	mutated := mutating.DeepCopy()

	err := cache.Iterate(func(m Mutator) error {
		matches, err := m.Matches(mutated, gvk, ns)
		if err != nil {
			return err
		}
		if !matches {
			return nil
		}
		mutated, err = m.Mutate(mutated)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return mutated, nil
}

// GenerateJSONPatch generates a json patch from the original and mutated object
func GenerateJSONPatch(original, mutated unstructured.Unstructured) ([]jsonpatch.JsonPatchOperation, error) {

	oldJSON, err := json.Marshal(original.Object)
	if err != nil {
		return nil, err
	}
	newJSON, err := json.Marshal(mutated.Object)
	if err != nil {
		return nil, err
	}

	patches, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	return patches, err
}
