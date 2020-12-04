package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func GetSelfLink(obj unstructured.Unstructured) string {
	return fmt.Sprintf("/apis/%s/%s/%s", obj.GetAPIVersion(), obj.GetKind(), obj.GetName())
}
