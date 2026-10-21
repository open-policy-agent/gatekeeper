package target

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
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

// AugmentedUnstructured is an Object to review, its Namespace (if known),
// its admission operation (when sourced from an admission review), and its
// source type.
type AugmentedUnstructured struct {
	Object           unstructured.Unstructured
	Namespace        *corev1.Namespace
	Operation        admissionv1.Operation
	Source           types.SourceType
	EnforcementPoint string
}
