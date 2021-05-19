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

package dynamiccache_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	kcorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kcache "k8s.io/client-go/tools/cache"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
)

const testNodeOne = "test-node-1"
const testNamespaceOne = "test-namespace-1"
const testNamespaceTwo = "test-namespace-2"
const testNamespaceThree = "test-namespace-3"

// TODO(community): Pull these helper functions into testenv.
// Restart policy is included to allow indexing on that field.
func createPod(name, namespace string, restartPolicy kcorev1.RestartPolicy) client.Object {
	three := int64(3)
	pod := &kcorev1.Pod{
		ObjectMeta: kmetav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"test-label": name,
			},
		},
		Spec: kcorev1.PodSpec{
			Containers:            []kcorev1.Container{{Name: "nginx", Image: "nginx"}},
			RestartPolicy:         restartPolicy,
			ActiveDeadlineSeconds: &three,
		},
	}
	cl, err := client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	err = cl.Create(context.Background(), pod)
	Expect(err).NotTo(HaveOccurred())
	return pod
}

func deletePod(pod client.Object) {
	cl, err := client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	err = cl.Delete(context.Background(), pod)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Informer Cache", func() {
	CacheTest(dynamiccache.New)
})
var _ = Describe("Multi-Namespace Informer Cache", func() {
	CacheTest(dynamiccache.MultiNamespacedCacheBuilder([]string{testNamespaceOne, testNamespaceTwo, "default"}))
})

// nolint: gocyclo
func CacheTest(createCacheFunc func(config *rest.Config, opts cache.Options) (cache.Cache, error)) {
	Describe("Cache test", func() {
		var (
			informerCache       cache.Cache
			informerCacheCtx    context.Context
			informerCacheCancel context.CancelFunc
			knownPod1           client.Object
			knownPod2           client.Object
			knownPod3           client.Object
			knownPod4           client.Object
		)

		BeforeEach(func() {
			informerCacheCtx, informerCacheCancel = context.WithCancel(context.Background())
			Expect(cfg).NotTo(BeNil())

			By("creating three pods")
			cl, err := client.New(cfg, client.Options{})
			Expect(err).NotTo(HaveOccurred())
			err = ensureNode(testNodeOne, cl)
			Expect(err).NotTo(HaveOccurred())
			err = ensureNamespace(testNamespaceOne, cl)
			Expect(err).NotTo(HaveOccurred())
			err = ensureNamespace(testNamespaceTwo, cl)
			Expect(err).NotTo(HaveOccurred())
			err = ensureNamespace(testNamespaceThree, cl)
			Expect(err).NotTo(HaveOccurred())
			// Includes restart policy since these objects are indexed on this field.
			knownPod1 = createPod("test-pod-1", testNamespaceOne, kcorev1.RestartPolicyNever)
			knownPod2 = createPod("test-pod-2", testNamespaceTwo, kcorev1.RestartPolicyAlways)
			knownPod3 = createPod("test-pod-3", testNamespaceTwo, kcorev1.RestartPolicyOnFailure)
			knownPod4 = createPod("test-pod-4", testNamespaceThree, kcorev1.RestartPolicyNever)
			podGVK := schema.GroupVersionKind{
				Kind:    "Pod",
				Version: "v1",
			}
			knownPod1.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod2.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod3.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod4.GetObjectKind().SetGroupVersionKind(podGVK)

			By("creating the informer cache")
			informerCache, err = createCacheFunc(cfg, cache.Options{})
			Expect(err).NotTo(HaveOccurred())
			By("running the cache and waiting for it to sync")
			// pass as an arg so that we don't race between close and re-assign
			go func(ctx context.Context) {
				defer GinkgoRecover()
				Expect(informerCache.Start(ctx)).To(Succeed())
			}(informerCacheCtx)
			Expect(informerCache.WaitForCacheSync(informerCacheCtx)).To(BeTrue())
		})

		AfterEach(func() {
			By("cleaning up created pods")
			deletePod(knownPod1)
			deletePod(knownPod2)
			deletePod(knownPod3)
			deletePod(knownPod4)

			informerCacheCancel()
		})

		Describe("as a Reader", func() {
			Context("with structured objects", func() {

				It("should be able to list objects that haven't been watched previously", func() {
					By("listing all services in the cluster")
					listObj := &kcorev1.ServiceList{}
					Expect(informerCache.List(context.Background(), listObj)).To(Succeed())

					By("verifying that the returned list contains the Kubernetes service")
					// NB: kubernetes default service is automatically created in testenv.
					Expect(listObj.Items).NotTo(BeEmpty())
					hasKubeService := false
					for _, svc := range listObj.Items {
						if isKubeService(&svc) {
							hasKubeService = true
							break
						}
					}
					Expect(hasKubeService).To(BeTrue())
				})

				It("should be able to get objects that haven't been watched previously", func() {
					By("getting the Kubernetes service")
					svc := &kcorev1.Service{}
					svcKey := client.ObjectKey{Namespace: "default", Name: "kubernetes"}
					Expect(informerCache.Get(context.Background(), svcKey, svc)).To(Succeed())

					By("verifying that the returned service looks reasonable")
					Expect(svc.Name).To(Equal("kubernetes"))
					Expect(svc.Namespace).To(Equal("default"))
				})

				It("should support filtering by labels in a single namespace", func() {
					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := kcorev1.PodList{}
					Expect(informerCache.List(context.Background(), &out,
						client.InNamespace(testNamespaceTwo),
						client.MatchingLabels(map[string]string{"test-label": "test-pod-2"}))).To(Succeed())

					By("verifying the returned pods have the correct label")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(1))
					actual := out.Items[0]
					Expect(actual.Labels["test-label"]).To(Equal("test-pod-2"))
				})

				It("should support filtering by labels from multiple namespaces", func() {
					By("creating another pod with the same label but different namespace")
					anotherPod := createPod("test-pod-2", testNamespaceOne, kcorev1.RestartPolicyAlways)
					defer deletePod(anotherPod)

					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := kcorev1.PodList{}
					labels := map[string]string{"test-label": "test-pod-2"}
					Expect(informerCache.List(context.Background(), &out, client.MatchingLabels(labels))).To(Succeed())

					By("verifying multiple pods with the same label in different namespaces are returned")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(2))
					for _, actual := range out.Items {
						Expect(actual.Labels["test-label"]).To(Equal("test-pod-2"))
					}
				})

				It("should be able to list objects with GVK populated", func() {
					By("listing pods")
					out := &kcorev1.PodList{}
					Expect(informerCache.List(context.Background(), out)).To(Succeed())

					By("verifying that the returned pods have GVK populated")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(SatisfyAny(HaveLen(3), HaveLen(4)))
					for _, p := range out.Items {
						Expect(p.GroupVersionKind()).To(Equal(kcorev1.SchemeGroupVersion.WithKind("Pod")))
					}
				})

				It("should be able to list objects by namespace", func() {
					By("listing pods in test-namespace-1")
					listObj := &kcorev1.PodList{}
					Expect(informerCache.List(context.Background(), listObj,
						client.InNamespace(testNamespaceOne))).To(Succeed())

					By("verifying that the returned pods are in test-namespace-1")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.Namespace).To(Equal(testNamespaceOne))
				})

				It("should deep copy the object unless told otherwise", func() {
					By("retrieving a specific pod from the cache")
					out := &kcorev1.Pod{}
					podKey := client.ObjectKey{Name: "test-pod-2", Namespace: testNamespaceTwo}
					Expect(informerCache.Get(context.Background(), podKey, out)).To(Succeed())

					By("verifying the retrieved pod is equal to a known pod")
					Expect(out).To(Equal(knownPod2))

					By("altering a field in the retrieved pod")
					*out.Spec.ActiveDeadlineSeconds = 4

					By("verifying the pods are no longer equal")
					Expect(out).NotTo(Equal(knownPod2))
				})

				It("should return an error if the object is not found", func() {
					By("getting a service that does not exists")
					svc := &kcorev1.Service{}
					svcKey := client.ObjectKey{Namespace: testNamespaceOne, Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
					Expect(errors.IsNotFound(err)).To(BeTrue())
				})

				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &kcorev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})

				It("should return an error when context is canceled", func() {
					By("canceling the context")
					informerCacheCancel()

					By("listing pods in test-namespace-1 with a canceled context")
					listObj := &kcorev1.PodList{}
					err := informerCache.List(informerCacheCtx, listObj, client.InNamespace(testNamespaceOne))

					By("verifying that an error is returned")
					Expect(err).To(HaveOccurred())
					Expect(errors.IsTimeout(err)).To(BeTrue())
				})
			})
			Context("with unstructured objects", func() {
				It("should be able to list objects that haven't been watched previously", func() {
					By("listing all services in the cluster")
					listObj := &unstructured.UnstructuredList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "ServiceList",
					})
					err := informerCache.List(context.Background(), listObj)
					Expect(err).To(Succeed())

					By("verifying that the returned list contains the Kubernetes service")
					// NB: kubernetes default service is automatically created in testenv.
					Expect(listObj.Items).NotTo(BeEmpty())
					hasKubeService := false
					for _, svc := range listObj.Items {
						if isKubeService(&svc) {
							hasKubeService = true
							break
						}
					}
					Expect(hasKubeService).To(BeTrue())
				})
				It("should be able to get objects that haven't been watched previously", func() {
					By("getting the Kubernetes service")
					svc := &unstructured.Unstructured{}
					svc.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					})
					svcKey := client.ObjectKey{Namespace: "default", Name: "kubernetes"}
					Expect(informerCache.Get(context.Background(), svcKey, svc)).To(Succeed())

					By("verifying that the returned service looks reasonable")
					Expect(svc.GetName()).To(Equal("kubernetes"))
					Expect(svc.GetNamespace()).To(Equal("default"))
				})

				It("should support filtering by labels in a single namespace", func() {
					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := unstructured.UnstructuredList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err := informerCache.List(context.Background(), &out,
						client.InNamespace(testNamespaceTwo),
						client.MatchingLabels(map[string]string{"test-label": "test-pod-2"}))
					Expect(err).To(Succeed())

					By("verifying the returned pods have the correct label")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(1))
					actual := out.Items[0]
					Expect(actual.GetLabels()["test-label"]).To(Equal("test-pod-2"))
				})

				It("should support filtering by labels from multiple namespaces", func() {
					By("creating another pod with the same label but different namespace")
					anotherPod := createPod("test-pod-2", testNamespaceOne, kcorev1.RestartPolicyAlways)
					defer deletePod(anotherPod)

					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := unstructured.UnstructuredList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					labels := map[string]string{"test-label": "test-pod-2"}
					err := informerCache.List(context.Background(), &out, client.MatchingLabels(labels))
					Expect(err).To(Succeed())

					By("verifying multiple pods with the same label in different namespaces are returned")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(2))
					for _, actual := range out.Items {
						Expect(actual.GetLabels()["test-label"]).To(Equal("test-pod-2"))
					}

				})

				It("should be able to list objects by namespace", func() {
					By("listing pods in test-namespace-1")
					listObj := &unstructured.UnstructuredList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err := informerCache.List(context.Background(), listObj, client.InNamespace(testNamespaceOne))
					Expect(err).To(Succeed())

					By("verifying that the returned pods are in test-namespace-1")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.GetNamespace()).To(Equal(testNamespaceOne))
				})

				It("should be able to restrict cache to a namespace", func() {
					By("creating a namespaced cache")
					namespacedCache, err := dynamiccache.New(cfg, cache.Options{Namespace: testNamespaceOne})
					Expect(err).NotTo(HaveOccurred())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(namespacedCache.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(namespacedCache.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing pods in all namespaces")
					out := &unstructured.UnstructuredList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					Expect(namespacedCache.List(context.Background(), out)).To(Succeed())

					By("verifying the returned pod is from the watched namespace")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(1))
					Expect(out.Items[0].GetNamespace()).To(Equal(testNamespaceOne))

					By("listing all nodes - should still be able to list a cluster-scoped resource")
					nodeList := &unstructured.UnstructuredList{}
					nodeList.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "NodeList",
					})
					Expect(namespacedCache.List(context.Background(), nodeList)).To(Succeed())

					By("verifying the node list is not empty")
					Expect(nodeList.Items).NotTo(BeEmpty())

					By("getting a node - should still be able to get a cluster-scoped resource")
					node := &unstructured.Unstructured{}
					node.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Node",
					})

					By("verifying that getting the node works with an empty namespace")
					key1 := client.ObjectKey{Namespace: "", Name: testNodeOne}
					Expect(namespacedCache.Get(context.Background(), key1, node)).To(Succeed())

					By("verifying that the namespace is ignored when getting a cluster-scoped resource")
					key2 := client.ObjectKey{Namespace: "random", Name: testNodeOne}
					Expect(namespacedCache.Get(context.Background(), key2, node)).To(Succeed())
				})

				It("should deep copy the object unless told otherwise", func() {
					By("retrieving a specific pod from the cache")
					out := &unstructured.Unstructured{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					uKnownPod2 := &unstructured.Unstructured{}
					Expect(kscheme.Scheme.Convert(knownPod2, uKnownPod2, nil)).To(Succeed())

					podKey := client.ObjectKey{Name: "test-pod-2", Namespace: testNamespaceTwo}
					Expect(informerCache.Get(context.Background(), podKey, out)).To(Succeed())

					By("verifying the retrieved pod is equal to a known pod")
					Expect(out).To(Equal(uKnownPod2))

					By("altering a field in the retrieved pod")
					m, _ := out.Object["spec"].(map[string]interface{})
					m["activeDeadlineSeconds"] = 4

					By("verifying the pods are no longer equal")
					Expect(out).NotTo(Equal(knownPod2))
				})

				It("should return an error if the object is not found", func() {
					By("getting a service that does not exists")
					svc := &unstructured.Unstructured{}
					svc.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					})
					svcKey := client.ObjectKey{Namespace: testNamespaceOne, Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
					Expect(errors.IsNotFound(err)).To(BeTrue())
				})
				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &kcorev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})
			})
			Context("with metadata-only objects", func() {
				It("should be able to list objects that haven't been watched previously", func() {
					By("listing all services in the cluster")
					listObj := &kmetav1.PartialObjectMetadataList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "ServiceList",
					})
					err := informerCache.List(context.Background(), listObj)
					Expect(err).To(Succeed())

					By("verifying that the returned list contains the Kubernetes service")
					// NB: kubernetes default service is automatically created in testenv.
					Expect(listObj.Items).NotTo(BeEmpty())
					hasKubeService := false
					for _, svc := range listObj.Items {
						if isKubeService(&svc) {
							hasKubeService = true
							break
						}
					}
					Expect(hasKubeService).To(BeTrue())
				})
				It("should be able to get objects that haven't been watched previously", func() {
					By("getting the Kubernetes service")
					svc := &kmetav1.PartialObjectMetadata{}
					svc.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					})
					svcKey := client.ObjectKey{Namespace: "default", Name: "kubernetes"}
					Expect(informerCache.Get(context.Background(), svcKey, svc)).To(Succeed())

					By("verifying that the returned service looks reasonable")
					Expect(svc.GetName()).To(Equal("kubernetes"))
					Expect(svc.GetNamespace()).To(Equal("default"))
				})

				It("should support filtering by labels in a single namespace", func() {
					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := kmetav1.PartialObjectMetadataList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err := informerCache.List(context.Background(), &out,
						client.InNamespace(testNamespaceTwo),
						client.MatchingLabels(map[string]string{"test-label": "test-pod-2"}))
					Expect(err).To(Succeed())

					By("verifying the returned pods have the correct label")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(1))
					actual := out.Items[0]
					Expect(actual.GetLabels()["test-label"]).To(Equal("test-pod-2"))
				})

				It("should support filtering by labels from multiple namespaces", func() {
					By("creating another pod with the same label but different namespace")
					anotherPod := createPod("test-pod-2", testNamespaceOne, kcorev1.RestartPolicyAlways)
					defer deletePod(anotherPod)

					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := kmetav1.PartialObjectMetadataList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					labels := map[string]string{"test-label": "test-pod-2"}
					err := informerCache.List(context.Background(), &out, client.MatchingLabels(labels))
					Expect(err).To(Succeed())

					By("verifying multiple pods with the same label in different namespaces are returned")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(2))
					for _, actual := range out.Items {
						Expect(actual.GetLabels()["test-label"]).To(Equal("test-pod-2"))
					}

				})

				It("should be able to list objects by namespace", func() {
					By("listing pods in test-namespace-1")
					listObj := &kmetav1.PartialObjectMetadataList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err := informerCache.List(context.Background(), listObj, client.InNamespace(testNamespaceOne))
					Expect(err).To(Succeed())

					By("verifying that the returned pods are in test-namespace-1")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.GetNamespace()).To(Equal(testNamespaceOne))
				})

				It("should be able to restrict cache to a namespace", func() {
					By("creating a namespaced cache")
					namespacedCache, err := dynamiccache.New(cfg, cache.Options{Namespace: testNamespaceOne})
					Expect(err).NotTo(HaveOccurred())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(namespacedCache.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(namespacedCache.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing pods in all namespaces")
					out := &kmetav1.PartialObjectMetadataList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					Expect(namespacedCache.List(context.Background(), out)).To(Succeed())

					By("verifying the returned pod is from the watched namespace")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(1))
					Expect(out.Items[0].GetNamespace()).To(Equal(testNamespaceOne))

					By("listing all nodes - should still be able to list a cluster-scoped resource")
					nodeList := &kmetav1.PartialObjectMetadataList{}
					nodeList.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "NodeList",
					})
					Expect(namespacedCache.List(context.Background(), nodeList)).To(Succeed())

					By("verifying the node list is not empty")
					Expect(nodeList.Items).NotTo(BeEmpty())

					By("getting a node - should still be able to get a cluster-scoped resource")
					node := &kmetav1.PartialObjectMetadata{}
					node.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Node",
					})

					By("verifying that getting the node works with an empty namespace")
					key1 := client.ObjectKey{Namespace: "", Name: testNodeOne}
					Expect(namespacedCache.Get(context.Background(), key1, node)).To(Succeed())

					By("verifying that the namespace is ignored when getting a cluster-scoped resource")
					key2 := client.ObjectKey{Namespace: "random", Name: testNodeOne}
					Expect(namespacedCache.Get(context.Background(), key2, node)).To(Succeed())
				})

				It("should deep copy the object unless told otherwise", func() {
					By("retrieving a specific pod from the cache")
					out := &kmetav1.PartialObjectMetadata{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					uKnownPod2 := &kmetav1.PartialObjectMetadata{}
					knownPod2.(*kcorev1.Pod).ObjectMeta.DeepCopyInto(&uKnownPod2.ObjectMeta)
					uKnownPod2.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})

					podKey := client.ObjectKey{Name: "test-pod-2", Namespace: testNamespaceTwo}
					Expect(informerCache.Get(context.Background(), podKey, out)).To(Succeed())

					By("verifying the retrieved pod is equal to a known pod")
					Expect(out).To(Equal(uKnownPod2))

					By("altering a field in the retrieved pod")
					out.Labels["foo"] = "bar"

					By("verifying the pods are no longer equal")
					Expect(out).NotTo(Equal(knownPod2))
				})

				It("should return an error if the object is not found", func() {
					By("getting a service that does not exists")
					svc := &kmetav1.PartialObjectMetadata{}
					svc.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					})
					svcKey := client.ObjectKey{Namespace: testNamespaceOne, Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
					Expect(errors.IsNotFound(err)).To(BeTrue())
				})
				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &kcorev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})
			})
		})
		Describe("as an Informer", func() {
			Context("with structured objects", func() {
				It("should be able to get informer for the object", func(done Done) {
					By("getting a shared index informer for a pod")
					pod := &kcorev1.Pod{
						ObjectMeta: kmetav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: kcorev1.PodSpec{
							Containers: []kcorev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					}
					sii, err := informerCache.GetInformer(context.TODO(), pod)
					Expect(err).NotTo(HaveOccurred())
					Expect(sii).NotTo(BeNil())
					Expect(sii.HasSynced()).To(BeTrue())

					By("adding an event handler listening for object creation which sends the object to a channel")
					out := make(chan interface{})
					addFunc := func(obj interface{}) {
						out <- obj
					}
					sii.AddEventHandler(kcache.ResourceEventHandlerFuncs{AddFunc: addFunc})

					By("adding an object")
					cl, err := client.New(cfg, client.Options{})
					Expect(err).NotTo(HaveOccurred())
					Expect(cl.Create(context.Background(), pod)).To(Succeed())
					defer deletePod(pod)

					By("verifying the object is received on the channel")
					Eventually(out).Should(Receive(Equal(pod)))
					close(done)
				})
				It("should be able to get an informer by group/version/kind", func(done Done) {
					By("getting an shared index informer for gvk = core/v1/pod")
					gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
					sii, err := informerCache.GetInformerForKind(context.TODO(), gvk)
					Expect(err).NotTo(HaveOccurred())
					Expect(sii).NotTo(BeNil())
					Expect(sii.HasSynced()).To(BeTrue())

					By("adding an event handler listening for object creation which sends the object to a channel")
					out := make(chan interface{})
					addFunc := func(obj interface{}) {
						out <- obj
					}
					sii.AddEventHandler(kcache.ResourceEventHandlerFuncs{AddFunc: addFunc})

					By("adding an object")
					cl, err := client.New(cfg, client.Options{})
					Expect(err).NotTo(HaveOccurred())
					pod := &kcorev1.Pod{
						ObjectMeta: kmetav1.ObjectMeta{
							Name:      "informer-gvk",
							Namespace: "default",
						},
						Spec: kcorev1.PodSpec{
							Containers: []kcorev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					}
					Expect(cl.Create(context.Background(), pod)).To(Succeed())
					defer deletePod(pod)

					By("verifying the object is received on the channel")
					Eventually(out).Should(Receive(Equal(pod)))
					close(done)
				})

				It("should be able to index an object field then retrieve objects by that field", func() {
					By("creating the cache")
					informer, err := dynamiccache.New(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("indexing the restartPolicy field of the Pod object before starting")
					pod := &kcorev1.Pod{}
					indexFunc := func(obj client.Object) []string {
						return []string{string(obj.(*kcorev1.Pod).Spec.RestartPolicy)}
					}
					Expect(informer.IndexField(context.TODO(), pod, "spec.restartPolicy", indexFunc)).To(Succeed())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(informer.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(informer.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing Pods with restartPolicyOnFailure")
					listObj := &kcorev1.PodList{}
					Expect(informer.List(context.Background(), listObj,
						client.MatchingFields{"spec.restartPolicy": "OnFailure"})).To(Succeed())
					By("verifying that the returned pods have correct restart policy")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.Name).To(Equal("test-pod-3"))
				})

				It("should allow for get informer to be canceled", func() {
					By("creating a context and canceling it")
					informerCacheCancel()

					By("getting a shared index informer for a pod with a canceled context")
					pod := &kcorev1.Pod{
						ObjectMeta: kmetav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: kcorev1.PodSpec{
							Containers: []kcorev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					}
					sii, err := informerCache.GetInformer(informerCacheCtx, pod)
					Expect(err).To(HaveOccurred())
					Expect(sii).To(BeNil())
					Expect(errors.IsTimeout(err)).To(BeTrue())
				})

				It("should allow getting an informer by group/version/kind to be canceled", func() {
					By("creating a context and canceling it")
					informerCacheCancel()

					By("getting an shared index informer for gvk = core/v1/pod with a canceled context")
					gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
					sii, err := informerCache.GetInformerForKind(informerCacheCtx, gvk)
					Expect(err).To(HaveOccurred())
					Expect(sii).To(BeNil())
					Expect(errors.IsTimeout(err)).To(BeTrue())
				})
			})
			Context("with unstructured objects", func() {
				It("should be able to get informer for the object", func(done Done) {
					By("getting a shared index informer for a pod")

					pod := &unstructured.Unstructured{
						Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []map[string]interface{}{
									{
										"name":  "nginx",
										"image": "nginx",
									},
								},
							},
						},
					}
					pod.SetName("informer-obj2")
					pod.SetNamespace("default")
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					sii, err := informerCache.GetInformer(context.TODO(), pod)
					Expect(err).NotTo(HaveOccurred())
					Expect(sii).NotTo(BeNil())
					Expect(sii.HasSynced()).To(BeTrue())

					By("adding an event handler listening for object creation which sends the object to a channel")
					out := make(chan interface{})
					addFunc := func(obj interface{}) {
						out <- obj
					}
					sii.AddEventHandler(kcache.ResourceEventHandlerFuncs{AddFunc: addFunc})

					By("adding an object")
					cl, err := client.New(cfg, client.Options{})
					Expect(err).NotTo(HaveOccurred())
					Expect(cl.Create(context.Background(), pod)).To(Succeed())
					defer deletePod(pod)

					By("verifying the object is received on the channel")
					Eventually(out).Should(Receive(Equal(pod)))
					close(done)
				}, 3)

				It("should be able to index an object field then retrieve objects by that field", func() {
					By("creating the cache")
					informer, err := dynamiccache.New(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("indexing the restartPolicy field of the Pod object before starting")
					pod := &unstructured.Unstructured{}
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					indexFunc := func(obj client.Object) []string {
						s, ok := obj.(*unstructured.Unstructured).Object["spec"]
						if !ok {
							return []string{}
						}
						m, ok := s.(map[string]interface{})
						if !ok {
							return []string{}
						}
						return []string{fmt.Sprintf("%v", m["restartPolicy"])}
					}
					Expect(informer.IndexField(context.TODO(), pod, "spec.restartPolicy", indexFunc)).To(Succeed())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(informer.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(informer.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing Pods with restartPolicyOnFailure")
					listObj := &unstructured.UnstructuredList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err = informer.List(context.Background(), listObj,
						client.MatchingFields{"spec.restartPolicy": "OnFailure"})
					Expect(err).To(Succeed())

					By("verifying that the returned pods have correct restart policy")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.GetName()).To(Equal("test-pod-3"))
				}, 3)

				It("should allow for get informer to be canceled", func() {
					By("canceling the context")
					informerCacheCancel()

					By("getting a shared index informer for a pod with a canceled context")
					pod := &unstructured.Unstructured{}
					pod.SetName("informer-obj2")
					pod.SetNamespace("default")
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					sii, err := informerCache.GetInformer(informerCacheCtx, pod)
					Expect(err).To(HaveOccurred())
					Expect(sii).To(BeNil())
					Expect(errors.IsTimeout(err)).To(BeTrue())
				})
			})
			Context("with metadata-only objects", func() {
				It("should be able to get informer for the object", func(done Done) {
					By("getting a shared index informer for a pod")

					pod := &kcorev1.Pod{
						ObjectMeta: kmetav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: kcorev1.PodSpec{
							Containers: []kcorev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					}

					podMeta := &kmetav1.PartialObjectMetadata{}
					pod.ObjectMeta.DeepCopyInto(&podMeta.ObjectMeta)
					podMeta.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})

					sii, err := informerCache.GetInformer(context.TODO(), podMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(sii).NotTo(BeNil())
					Expect(sii.HasSynced()).To(BeTrue())

					By("adding an event handler listening for object creation which sends the object to a channel")
					out := make(chan interface{})
					addFunc := func(obj interface{}) {
						out <- obj
					}
					sii.AddEventHandler(kcache.ResourceEventHandlerFuncs{AddFunc: addFunc})

					By("adding an object")
					cl, err := client.New(cfg, client.Options{})
					Expect(err).NotTo(HaveOccurred())
					Expect(cl.Create(context.Background(), pod)).To(Succeed())
					defer deletePod(pod)
					// re-copy the result in so that we can match on it properly
					pod.ObjectMeta.DeepCopyInto(&podMeta.ObjectMeta)
					// NB(directxman12): proto doesn't care typemeta, and
					// partialobjectmetadata is proto, so no typemeta
					// TODO(directxman12): we should paper over this in controller-runtime
					podMeta.APIVersion = ""
					podMeta.Kind = ""

					By("verifying the object's metadata is received on the channel")
					Eventually(out).Should(Receive(Equal(podMeta)))
					close(done)
				}, 3)

				It("should be able to index an object field then retrieve objects by that field", func() {
					By("creating the cache")
					informer, err := dynamiccache.New(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("indexing the restartPolicy field of the Pod object before starting")
					pod := &kmetav1.PartialObjectMetadata{}
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					indexFunc := func(obj client.Object) []string {
						metadata := obj.(*kmetav1.PartialObjectMetadata)
						return []string{metadata.Labels["test-label"]}
					}
					Expect(informer.IndexField(context.TODO(), pod, "metadata.labels.test-label", indexFunc)).To(Succeed())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(informer.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(informer.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing Pods with restartPolicyOnFailure")
					listObj := &kmetav1.PartialObjectMetadataList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err = informer.List(context.Background(), listObj,
						client.MatchingFields{"metadata.labels.test-label": "test-pod-3"})
					Expect(err).To(Succeed())

					By("verifying that the returned pods have correct restart policy")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.GetName()).To(Equal("test-pod-3"))
				}, 3)

				It("should allow for get informer to be canceled", func() {
					By("creating a context and canceling it")
					ctx, cancel := context.WithCancel(context.Background())
					cancel()

					By("getting a shared index informer for a pod with a canceled context")
					pod := &kmetav1.PartialObjectMetadata{}
					pod.SetName("informer-obj2")
					pod.SetNamespace("default")
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					sii, err := informerCache.GetInformer(ctx, pod)
					Expect(err).To(HaveOccurred())
					Expect(sii).To(BeNil())
					Expect(errors.IsTimeout(err)).To(BeTrue())
				})
			})
		})
	})
}

// ensureNamespace installs namespace of a given name if not exists
func ensureNamespace(namespace string, client client.Client) error {
	ns := kcorev1.Namespace{
		ObjectMeta: kmetav1.ObjectMeta{
			Name: namespace,
		},
		TypeMeta: kmetav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
	}
	err := client.Create(context.TODO(), &ns)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func ensureNode(name string, client client.Client) error {
	node := kcorev1.Node{
		ObjectMeta: kmetav1.ObjectMeta{
			Name: name,
		},
		TypeMeta: kmetav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
	}
	err := client.Create(context.TODO(), &node)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

//nolint:interfacer
func isKubeService(svc kmetav1.Object) bool {
	// grumble grumble linters grumble grumble
	return svc.GetNamespace() == "default" && svc.GetName() == "kubernetes"
}
