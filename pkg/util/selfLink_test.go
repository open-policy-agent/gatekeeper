package util

import (
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetUniqueKey(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
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
	expected := KindVersionResource{
		version:  "constraints.gatekeeper.sh/v1beta1",
		kind:     "myTemplate",
		resource: "myConstraint",
	}
	g.Expect(key).To(gomega.Equal(expected))
}
