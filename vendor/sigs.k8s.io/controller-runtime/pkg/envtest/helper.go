package envtest

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// mergePaths merges two string slices containing paths.
// This function makes no guarantees about order of the merged slice.
func mergePaths(s1, s2 []string) []string {
	m := make(map[string]struct{})
	for _, s := range s1 {
		m[s] = struct{}{}
	}
	for _, s := range s2 {
		m[s] = struct{}{}
	}
	merged := make([]string, len(m))
	i := 0
	for key := range m {
		merged[i] = key
		i++
	}
	return merged
}

// mergeCRDs merges two CRD slices using their names.
// This function makes no guarantees about order of the merged slice.
func mergeCRDs(s1, s2 []runtime.Object) []runtime.Object {
	m := make(map[string]*unstructured.Unstructured)
	for _, obj := range runtimeListToUnstructured(s1) {
		m[obj.GetName()] = obj
	}
	for _, obj := range runtimeListToUnstructured(s2) {
		m[obj.GetName()] = obj
	}
	merged := make([]runtime.Object, len(m))
	i := 0
	for _, obj := range m {
		merged[i] = obj
		i++
	}
	return merged
}

// existsUnstructured verify if a any item is common between two lists.
func existsUnstructured(s1, s2 []*unstructured.Unstructured) bool {
	for _, s1obj := range s1 {
		for _, s2obj := range s2 {
			if reflect.DeepEqual(s1obj, s2obj) {
				return true
			}
		}
	}
	return false
}

func runtimeListToUnstructured(l []runtime.Object) []*unstructured.Unstructured {
	res := []*unstructured.Unstructured{}
	for _, obj := range l {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.DeepCopyObject())
		if err != nil {
			continue
		}
		res = append(res, &unstructured.Unstructured{
			Object: m,
		})
	}
	return res
}

func unstructuredListToRuntime(l []*unstructured.Unstructured) []runtime.Object {
	res := []runtime.Object{}
	for _, obj := range l {
		res = append(res, obj.DeepCopy())
	}
	return res
}
