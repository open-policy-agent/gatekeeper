package mutation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestJSONPatch(t *testing.T) {
	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "foo",
		},
	}
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "foo",
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				"fooa": "bara",
			},
		},
	}
	oldUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(oldPod)
	if err != nil {
		t.Errorf("Convert old pod to unstructured failed")
	}
	newUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(newPod)
	if err != nil {
		t.Errorf("Convert new pod to unstructured failed")
	}

	patches, err := mutation.GenerateJSONPatch(
		unstructured.Unstructured{Object: oldUnstructured},
		unstructured.Unstructured{Object: newUnstructured})
	if err != nil {
		t.Errorf("Generate JSON failed")
	}

	expectedPatches := []jsonpatch.Operation{
		{
			Operation: "add",
			Path:      "/metadata/annotations",
			Value: map[string]interface{}{
				"fooa": "bara",
			},
		},
		{
			Operation: "add",
			Path:      "/metadata/labels",
			Value: map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	if len(patches) != len(expectedPatches) {
		t.Errorf("Patches are not the same length %v %v", patches, expectedPatches)
	}

	for _, p := range patches {
		found := false
		for _, q := range expectedPatches {
			if cmp.Equal(p, q) {
				found = true
			}
		}
		if !found {
			t.Errorf("Expected to found %v in %v", p, expectedPatches)
		}
	}
}
