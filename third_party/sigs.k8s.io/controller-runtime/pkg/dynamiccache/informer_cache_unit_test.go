/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Modified from the original source (available at
// https://github.com/kubernetes-sigs/controller-runtime/tree/v0.8.2/pkg/cache)

package dynamiccache

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache/internal"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	crscheme "sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	itemPointerSliceTypeGroupName = "jakob.fabian"
	itemPointerSliceTypeVersion   = "v1"
)

var _ = Describe("ip.objectTypeForListObject", func() {
	ip := &dynamicInformerCache{
		InformersMap: &internal.InformersMap{Scheme: scheme.Scheme},
	}

	It("should find the object type for unstructured lists", func() {
		unstructuredList := &unstructured.UnstructuredList{}
		unstructuredList.SetAPIVersion("v1")
		unstructuredList.SetKind("PodList")

		gvk, obj, err := ip.objectTypeForListObject(unstructuredList)
		Expect(err).ToNot(HaveOccurred())
		Expect(gvk.Group).To(Equal(""))
		Expect(gvk.Version).To(Equal("v1"))
		Expect(gvk.Kind).To(Equal("Pod"))
		referenceUnstructured := &unstructured.Unstructured{}
		referenceUnstructured.SetGroupVersionKind(*gvk)
		Expect(obj).To(Equal(referenceUnstructured))

	})

	It("should find the object type of a list with a slice of literals items field", func() {
		gvk, obj, err := ip.objectTypeForListObject(&corev1.PodList{})
		Expect(err).ToNot(HaveOccurred())
		Expect(gvk.Group).To(Equal(""))
		Expect(gvk.Version).To(Equal("v1"))
		Expect(gvk.Kind).To(Equal("Pod"))
		var referencePod *corev1.Pod
		Expect(obj).To(Equal(referencePod))

	})

	It("should find the object type of a list with a slice of pointers items field", func() {
		By("registering the type", func() {
			ip.Scheme = runtime.NewScheme()
			err := (&crscheme.Builder{
				GroupVersion: schema.GroupVersion{Group: itemPointerSliceTypeGroupName, Version: itemPointerSliceTypeVersion},
			}).
				Register(
					&controllertest.UnconventionalListType{},
					&controllertest.UnconventionalListTypeList{},
				).AddToScheme(ip.Scheme)
			Expect(err).To(BeNil())
		})

		By("calling objectTypeForListObject", func() {
			gvk, obj, err := ip.objectTypeForListObject(&controllertest.UnconventionalListTypeList{})
			Expect(err).ToNot(HaveOccurred())
			Expect(gvk.Group).To(Equal(itemPointerSliceTypeGroupName))
			Expect(gvk.Version).To(Equal(itemPointerSliceTypeVersion))
			Expect(gvk.Kind).To(Equal("UnconventionalListType"))
			var referenceObject *controllertest.UnconventionalListType
			Expect(obj).To(Equal(referenceObject))
		})
	})
})
