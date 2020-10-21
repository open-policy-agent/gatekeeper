package mutation

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Mutator is a wrapper to runtime object that describe mutations.
type Mutator interface {
	Mutate(obj runtime.Object) (runtime.Object, error)
	Matches(scheme *runtime.Scheme, obj runtime.Object, ns *corev1.Namespace) (bool, error)
	Obj() runtime.Object
}
