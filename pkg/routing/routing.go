package routing

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("routing")

const (
	// API group for all PodStatus resources.
	StatusGroup = "status.gatekeeper.sh"
)

// IsManagementResource returns true for resources that should be on the management cluster.
func IsManagementResource(gvk schema.GroupVersionKind) bool {
	return gvk.Group == StatusGroup
}

// ResolveGVK infers the GVK from a runtime.Object.
func ResolveGVK(scheme *runtime.Scheme, obj runtime.Object) (schema.GroupVersionKind, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if !gvk.Empty() {
		return gvk, nil
	}
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("cannot resolve GVK for %T: %w", obj, err)
	}
	if len(gvks) == 0 {
		return schema.GroupVersionKind{}, fmt.Errorf("no GVK found for %T", obj)
	}
	return gvks[0], nil
}
