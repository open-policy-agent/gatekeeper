package fakes

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func UnstructuredFor(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	if namespace == "" {
		u.SetNamespace("default")
	} else {
		u.SetNamespace(namespace)
	}

	if gvk.Kind == "Pod" {
		u.Object["spec"] = map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name":  "foo-container",
					"image": "foo-image",
				},
			},
		}
	}

	return u
}
