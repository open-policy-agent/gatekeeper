package util

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetUniqueKey(t *testing.T) {
	gvk := schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "myTemplate",
	}
	name := "myConstraint"
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)

	key := GetUniqueKey(*obj)
	expected := KindVersionName{
		Group:     "constraints.gatekeeper.sh",
		Version:   "v1beta1",
		Kind:      "myTemplate",
		Name:      "myConstraint",
		Namespace: "",
	}

	if key != expected {
		t.Fatalf("got key %q, want %q", key, expected)
	}
}
