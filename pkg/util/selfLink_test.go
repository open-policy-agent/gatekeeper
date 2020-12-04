package util

import (
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetSelfLink(t *testing.T) {
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

	selfLink := GetSelfLink(*obj)
	expected := "/apis/constraints.gatekeeper.sh/v1beta1/myTemplate/myConstraint"
	g.Expect(selfLink).To(gomega.Equal(expected))
}
