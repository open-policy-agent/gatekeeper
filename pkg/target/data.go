package target

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type wipeData struct{}

// WipeData returns a value which, when passed to client.RemoveData(), wipes
// all cached data.
func WipeData() interface{} {
	return wipeData{}
}

func IsWipeData(o interface{}) bool {
	_, ok := o.(wipeData)
	return ok
}

// AugmentedUnstructured is an Object to review, and its Namespace (if known).
type AugmentedUnstructured struct {
	Object    unstructured.Unstructured
	Namespace *corev1.Namespace
}
