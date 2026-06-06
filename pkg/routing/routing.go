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
	statusGroup = "status.gatekeeper.sh"
)

// isManagementResource returns true for resources that should be on the management cluster.
func isManagementResource(gvk schema.GroupVersionKind) bool {
	return gvk.Group == statusGroup
}

// resolveGVK infers the GVK from a runtime.Object.
func resolveGVK(scheme *runtime.Scheme, obj runtime.Object) (schema.GroupVersionKind, error) {
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
