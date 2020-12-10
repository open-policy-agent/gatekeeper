package util

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type KindVersionResource struct {
	kind     string
	version  string
	resource string
}

func GetUniqueKey(obj unstructured.Unstructured) KindVersionResource {
	return KindVersionResource{
		version:  obj.GetAPIVersion(),
		kind:     obj.GetKind(),
		resource: obj.GetName(),
	}
}
