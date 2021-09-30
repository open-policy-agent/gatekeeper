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
// https://github.com/kubernetes-sigs/controller-runtime/tree/v0.9.2/pkg/cache)

package dynamiccache_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
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
func createPodWithLabels(name, namespace string, restartPolicy corev1.RestartPolicy, labels map[string]string) client.Object {
	three := int64(3)
	if labels == nil {
		labels = map[string]string{}
	}
	labels["test-label"] = name
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers:            []corev1.Container{{Name: "nginx", Image: "nginx"}},
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

func createPod(name, namespace string, restartPolicy corev1.RestartPolicy) client.Object {
	return createPodWithLabels(name, namespace, restartPolicy, nil)
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
			knownPod5           client.Object
			knownPod6           client.Object
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
			knownPod1 = createPod("test-pod-1", testNamespaceOne, corev1.RestartPolicyNever)
			knownPod2 = createPod("test-pod-2", testNamespaceTwo, corev1.RestartPolicyAlways)
			knownPod3 = createPodWithLabels("test-pod-3", testNamespaceTwo, corev1.RestartPolicyOnFailure, map[string]string{"common-label": "common"})
			knownPod4 = createPodWithLabels("test-pod-4", testNamespaceThree, corev1.RestartPolicyNever, map[string]string{"common-label": "common"})
			knownPod5 = createPod("test-pod-5", testNamespaceOne, corev1.RestartPolicyNever)
			knownPod6 = createPod("test-pod-6", testNamespaceTwo, corev1.RestartPolicyAlways)

			podGVK := schema.GroupVersionKind{
				Kind:    "Pod",
				Version: "v1",
			}

			knownPod1.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod2.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod3.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod4.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod5.GetObjectKind().SetGroupVersionKind(podGVK)
			knownPod6.GetObjectKind().SetGroupVersionKind(podGVK)

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
			deletePod(knownPod5)
			deletePod(knownPod6)

			informerCacheCancel()
		})

		Describe("as a Reader", func() {
			Context("with structured objects", func() {
				It("should be able to list objects that haven't been watched previously", func() {
					By("listing all services in the cluster")
					listObj := &corev1.ServiceList{}
					Expect(informerCache.List(context.Background(), listObj)).To(Succeed())

					By("verifying that the returned list contains the Kubernetes service")
					// NB: kubernetes default service is automatically created in testenv.
					Expect(listObj.Items).NotTo(BeEmpty())
					hasKubeService := false
					for i := range listObj.Items {
						svc := &listObj.Items[i]
						if isKubeService(svc) {
							hasKubeService = true
							break
						}
					}
					Expect(hasKubeService).To(BeTrue())
				})

				It("should be able to get objects that haven't been watched previously", func() {
					By("getting the Kubernetes service")
					svc := &corev1.Service{}
					svcKey := client.ObjectKey{Namespace: "default", Name: "kubernetes"}
					Expect(informerCache.Get(context.Background(), svcKey, svc)).To(Succeed())

					By("verifying that the returned service looks reasonable")
					Expect(svc.Name).To(Equal("kubernetes"))
					Expect(svc.Namespace).To(Equal("default"))
				})

				It("should support filtering by labels in a single namespace", func() {
					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := corev1.PodList{}
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
					anotherPod := createPod("test-pod-2", testNamespaceOne, corev1.RestartPolicyAlways)
					defer deletePod(anotherPod)

					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := corev1.PodList{}
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
					out := &corev1.PodList{}
					Expect(informerCache.List(context.Background(), out)).To(Succeed())

					By("verifying that the returned pods have GVK populated")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(SatisfyAny(HaveLen(5), HaveLen(6)))
					for _, p := range out.Items {
						Expect(p.GroupVersionKind()).To(Equal(corev1.SchemeGroupVersion.WithKind("Pod")))
					}
				})

				It("should be able to list objects by namespace", func() {
					By("listing pods in test-namespace-1")
					listObj := &corev1.PodList{}
					Expect(informerCache.List(context.Background(), listObj,
						client.InNamespace(testNamespaceOne))).To(Succeed())

					By("verifying that the returned pods are in test-namespace-1")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(2))
					for _, item := range listObj.Items {
						Expect(item.Namespace).To(Equal(testNamespaceOne))
					}
				})

				It("should deep copy the object unless told otherwise", func() {
					By("retrieving a specific pod from the cache")
					out := &corev1.Pod{}
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
					svc := &corev1.Service{}
					svcKey := client.ObjectKey{Namespace: testNamespaceOne, Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
					Expect(apierrors.IsNotFound(err)).To(BeTrue())
				})

				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &corev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})

				It("should return an error when context is cancelled", func() {
					By("cancelling the context")
					informerCacheCancel()

					By("listing pods in test-namespace-1 with a cancelled context")
					listObj := &corev1.PodList{}
					err := informerCache.List(informerCacheCtx, listObj, client.InNamespace(testNamespaceOne))

					By("verifying that an error is returned")
					Expect(err).To(HaveOccurred())
					Expect(apierrors.IsTimeout(err)).To(BeTrue())
				})

				It("should set the Limit option and limit number of objects to Limit when List is called", func() {
					opts := &client.ListOptions{Limit: int64(3)}
					By("verifying that only Limit (3) number of objects are retrieved from the cache")
					listObj := &corev1.PodList{}
					Expect(informerCache.List(context.Background(), listObj, opts)).To(Succeed())
					Expect(listObj.Items).Should(HaveLen(3))
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
					for i := range listObj.Items {
						svc := &listObj.Items[i]
						if isKubeService(svc) {
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
					anotherPod := createPod("test-pod-2", testNamespaceOne, corev1.RestartPolicyAlways)
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
					Expect(listObj.Items).Should(HaveLen(2))
					for _, item := range listObj.Items {
						Expect(item.GetNamespace()).To(Equal(testNamespaceOne))
					}
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
					Expect(out.Items).Should(HaveLen(2))
					for _, item := range out.Items {
						Expect(item.GetNamespace()).To(Equal(testNamespaceOne))
					}
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
					Expect(apierrors.IsNotFound(err)).To(BeTrue())
				})
				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &corev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})
				It("test multinamespaced cache for cluster scoped resources", func() {
					By("creating a multinamespaced cache to watch specific namespaces")
					multi := cache.MultiNamespacedCacheBuilder([]string{"default", testNamespaceOne})
					m, err := multi(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("running the cache and waiting it for sync")
					go func() {
						defer GinkgoRecover()
						Expect(m.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(m.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("should be able to fetch cluster scoped resource")
					node := &corev1.Node{}

					By("verifying that getting the node works with an empty namespace")
					key1 := client.ObjectKey{Namespace: "", Name: testNodeOne}
					Expect(m.Get(context.Background(), key1, node)).To(Succeed())

					By("verifying if the cluster scoped resources are not duplicated")
					nodeList := &unstructured.UnstructuredList{}
					nodeList.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "NodeList",
					})
					Expect(m.List(context.Background(), nodeList)).To(Succeed())

					By("verifying the node list is not empty")
					Expect(nodeList.Items).NotTo(BeEmpty())
					Expect(len(nodeList.Items)).To(BeEquivalentTo(1))
				})
			})
			Context("with metadata-only objects", func() {
				It("should be able to list objects that haven't been watched previously", func() {
					By("listing all services in the cluster")
					listObj := &metav1.PartialObjectMetadataList{}
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
					for i := range listObj.Items {
						svc := &listObj.Items[i]
						if isKubeService(svc) {
							hasKubeService = true
							break
						}
					}
					Expect(hasKubeService).To(BeTrue())
				})
				It("should be able to get objects that haven't been watched previously", func() {
					By("getting the Kubernetes service")
					svc := &metav1.PartialObjectMetadata{}
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
					out := metav1.PartialObjectMetadataList{}
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
					anotherPod := createPod("test-pod-2", testNamespaceOne, corev1.RestartPolicyAlways)
					defer deletePod(anotherPod)

					By("listing pods with a particular label")
					// NB: each pod has a "test-label": <pod-name>
					out := metav1.PartialObjectMetadataList{}
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
					listObj := &metav1.PartialObjectMetadataList{}
					listObj.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					err := informerCache.List(context.Background(), listObj, client.InNamespace(testNamespaceOne))
					Expect(err).To(Succeed())

					By("verifying that the returned pods are in test-namespace-1")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(2))
					for _, item := range listObj.Items {
						Expect(item.Namespace).To(Equal(testNamespaceOne))
					}
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
					out := &metav1.PartialObjectMetadataList{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					})
					Expect(namespacedCache.List(context.Background(), out)).To(Succeed())

					By("verifying the returned pod is from the watched namespace")
					Expect(out.Items).NotTo(BeEmpty())
					Expect(out.Items).Should(HaveLen(2))
					for _, item := range out.Items {
						Expect(item.Namespace).To(Equal(testNamespaceOne))
					}
					By("listing all nodes - should still be able to list a cluster-scoped resource")
					nodeList := &metav1.PartialObjectMetadataList{}
					nodeList.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "NodeList",
					})
					Expect(namespacedCache.List(context.Background(), nodeList)).To(Succeed())

					By("verifying the node list is not empty")
					Expect(nodeList.Items).NotTo(BeEmpty())

					By("getting a node - should still be able to get a cluster-scoped resource")
					node := &metav1.PartialObjectMetadata{}
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
					out := &metav1.PartialObjectMetadata{}
					out.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					uKnownPod2 := &metav1.PartialObjectMetadata{}
					knownPod2.(*corev1.Pod).ObjectMeta.DeepCopyInto(&uKnownPod2.ObjectMeta)
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
					svc := &metav1.PartialObjectMetadata{}
					svc.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					})
					svcKey := client.ObjectKey{Namespace: testNamespaceOne, Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
					Expect(apierrors.IsNotFound(err)).To(BeTrue())
				})
				It("should return an error if getting object in unwatched namespace", func() {
					By("getting a service that does not exists")
					svc := &corev1.Service{}
					svcKey := client.ObjectKey{Namespace: "unknown", Name: "unknown"}

					By("verifying that an error is returned")
					err := informerCache.Get(context.Background(), svcKey, svc)
					Expect(err).To(HaveOccurred())
				})
			})
			type selectorsTestCase struct {
				fieldSelectors map[string]string
				labelSelectors map[string]string
				expectedPods   []string
			}
			DescribeTable(" and cache with selectors", func(tc selectorsTestCase) {
				By("creating the cache")
				builder := cache.BuilderWithOptions(
					cache.Options{
						SelectorsByObject: cache.SelectorsByObject{
							&corev1.Pod{}: {
								Label: labels.Set(tc.labelSelectors).AsSelector(),
								Field: fields.Set(tc.fieldSelectors).AsSelector(),
							},
						},
					},
				)
				informer, err := builder(cfg, cache.Options{})
				Expect(err).NotTo(HaveOccurred())

				By("running the cache and waiting for it to sync")
				go func() {
					defer GinkgoRecover()
					Expect(informer.Start(informerCacheCtx)).To(Succeed())
				}()
				Expect(informer.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

				By("Checking with structured")
				obtainedStructuredPodList := corev1.PodList{}
				Expect(informer.List(context.Background(), &obtainedStructuredPodList)).To(Succeed())
				Expect(obtainedStructuredPodList.Items).Should(WithTransform(func(pods []corev1.Pod) []string {
					obtainedPodNames := []string{}
					for _, pod := range pods {
						obtainedPodNames = append(obtainedPodNames, pod.Name)
					}
					return obtainedPodNames
				}, ConsistOf(tc.expectedPods)))

				By("Checking with unstructured")
				obtainedUnstructuredPodList := unstructured.UnstructuredList{}
				obtainedUnstructuredPodList.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "PodList",
				})
				err = informer.List(context.Background(), &obtainedUnstructuredPodList)
				Expect(err).To(Succeed())
				Expect(obtainedUnstructuredPodList.Items).Should(WithTransform(func(pods []unstructured.Unstructured) []string {
					obtainedPodNames := []string{}
					for _, pod := range pods {
						obtainedPodNames = append(obtainedPodNames, pod.GetName())
					}
					return obtainedPodNames
				}, ConsistOf(tc.expectedPods)))

				By("Checking with metadata")
				obtainedMetadataPodList := metav1.PartialObjectMetadataList{}
				obtainedMetadataPodList.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "PodList",
				})
				err = informer.List(context.Background(), &obtainedMetadataPodList)
				Expect(err).To(Succeed())
				Expect(obtainedMetadataPodList.Items).Should(WithTransform(func(pods []metav1.PartialObjectMetadata) []string {
					obtainedPodNames := []string{}
					for _, pod := range pods {
						obtainedPodNames = append(obtainedPodNames, pod.Name)
					}
					return obtainedPodNames
				}, ConsistOf(tc.expectedPods)))
			},
				Entry("when selectors are empty it has to inform about all the pods", selectorsTestCase{
					fieldSelectors: map[string]string{},
					labelSelectors: map[string]string{},
					expectedPods:   []string{"test-pod-1", "test-pod-2", "test-pod-3", "test-pod-4", "test-pod-5", "test-pod-6"},
				}),
				Entry("when field matches one pod it has to inform about it", selectorsTestCase{
					fieldSelectors: map[string]string{"metadata.name": "test-pod-2"},
					expectedPods:   []string{"test-pod-2"},
				}),
				Entry("when field matches multiple pods it has to inform about all of them", selectorsTestCase{
					fieldSelectors: map[string]string{"metadata.namespace": testNamespaceTwo},
					expectedPods:   []string{"test-pod-2", "test-pod-3", "test-pod-6"},
				}),
				Entry("when label matches one pod it has to inform about it", selectorsTestCase{
					labelSelectors: map[string]string{"test-label": "test-pod-4"},
					expectedPods:   []string{"test-pod-4"},
				}),
				Entry("when label matches multiple pods it has to inform about all of them", selectorsTestCase{
					labelSelectors: map[string]string{"common-label": "common"},
					expectedPods:   []string{"test-pod-3", "test-pod-4"},
				}),
				Entry("when label and field matches one pod it has to inform about about it", selectorsTestCase{
					labelSelectors: map[string]string{"common-label": "common"},
					fieldSelectors: map[string]string{"metadata.namespace": testNamespaceTwo},
					expectedPods:   []string{"test-pod-3"},
				}),
				Entry("when label does not match it does not has to inform", selectorsTestCase{
					labelSelectors: map[string]string{"new-label": "new"},
					expectedPods:   []string{},
				}),
				Entry("when field does not match it does not has to inform", selectorsTestCase{
					fieldSelectors: map[string]string{"metadata.namespace": "new"},
					expectedPods:   []string{},
				}),
			)
		})
		Describe("as an Informer", func() {
			Context("with structured objects", func() {
				It("should be able to get informer for the object", func() {
					By("getting a shared index informer for a pod")
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
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
				})
				It("should be able to get an informer by group/version/kind", func() {
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
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "informer-gvk",
							Namespace: "default",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
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
				})
				It("should be able to index an object field then retrieve objects by that field", func() {
					By("creating the cache")
					informer, err := dynamiccache.New(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("indexing the restartPolicy field of the Pod object before starting")
					pod := &corev1.Pod{}
					indexFunc := func(obj client.Object) []string {
						return []string{string(obj.(*corev1.Pod).Spec.RestartPolicy)}
					}
					Expect(informer.IndexField(context.TODO(), pod, "spec.restartPolicy", indexFunc)).To(Succeed())

					By("running the cache and waiting for it to sync")
					go func() {
						defer GinkgoRecover()
						Expect(informer.Start(informerCacheCtx)).To(Succeed())
					}()
					Expect(informer.WaitForCacheSync(informerCacheCtx)).NotTo(BeFalse())

					By("listing Pods with restartPolicyOnFailure")
					listObj := &corev1.PodList{}
					Expect(informer.List(context.Background(), listObj,
						client.MatchingFields{"spec.restartPolicy": "OnFailure"})).To(Succeed())
					By("verifying that the returned pods have correct restart policy")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.Name).To(Equal("test-pod-3"))
				})

				It("should allow for get informer to be cancelled", func() {
					By("creating a context and cancelling it")
					informerCacheCancel()

					By("getting a shared index informer for a pod with a cancelled context")
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
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
					Expect(apierrors.IsTimeout(err)).To(BeTrue())
				})

				It("should allow getting an informer by group/version/kind to be cancelled", func() {
					By("creating a context and cancelling it")
					informerCacheCancel()

					By("getting an shared index informer for gvk = core/v1/pod with a cancelled context")
					gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
					sii, err := informerCache.GetInformerForKind(informerCacheCtx, gvk)
					Expect(err).To(HaveOccurred())
					Expect(sii).To(BeNil())
					Expect(apierrors.IsTimeout(err)).To(BeTrue())
				})
			})
			Context("with unstructured objects", func() {
				It("should be able to get informer for the object", func() {
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

				It("should allow for get informer to be cancelled", func() {
					By("cancelling the context")
					informerCacheCancel()

					By("getting a shared index informer for a pod with a cancelled context")
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
					Expect(apierrors.IsTimeout(err)).To(BeTrue())
				})
			})
			Context("with metadata-only objects", func() {
				It("should be able to get informer for the object", func() {
					By("getting a shared index informer for a pod")

					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "informer-obj",
							Namespace: "default",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					}

					podMeta := &metav1.PartialObjectMetadata{}
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

					By("verifying the object's metadata is received on the channel")
					Eventually(out).Should(Receive(Equal(podMeta)))
				}, 3)

				It("should be able to index an object field then retrieve objects by that field", func() {
					By("creating the cache")
					informer, err := dynamiccache.New(cfg, cache.Options{})
					Expect(err).NotTo(HaveOccurred())

					By("indexing the restartPolicy field of the Pod object before starting")
					pod := &metav1.PartialObjectMetadata{}
					pod.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					})
					indexFunc := func(obj client.Object) []string {
						metadata := obj.(*metav1.PartialObjectMetadata)
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
					listObj := &metav1.PartialObjectMetadataList{}
					gvk := schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "PodList",
					}
					listObj.SetGroupVersionKind(gvk)
					err = informer.List(context.Background(), listObj,
						client.MatchingFields{"metadata.labels.test-label": "test-pod-3"})
					Expect(err).To(Succeed())

					By("verifying that the GVK has been preserved for the list object")
					Expect(listObj.GroupVersionKind()).To(Equal(gvk))

					By("verifying that the returned pods have correct restart policy")
					Expect(listObj.Items).NotTo(BeEmpty())
					Expect(listObj.Items).Should(HaveLen(1))
					actual := listObj.Items[0]
					Expect(actual.GetName()).To(Equal("test-pod-3"))

					By("verifying that the GVK has been preserved for the item in the list")
					Expect(actual.GroupVersionKind()).To(Equal(schema.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					}))
				}, 3)

				It("should allow for get informer to be cancelled", func() {
					By("creating a context and cancelling it")
					ctx, cancel := context.WithCancel(context.Background())
					cancel()

					By("getting a shared index informer for a pod with a cancelled context")
					pod := &metav1.PartialObjectMetadata{}
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
					Expect(apierrors.IsTimeout(err)).To(BeTrue())
				})
			})
		})
	})
}

// ensureNamespace installs namespace of a given name if not exists.
func ensureNamespace(namespace string, client client.Client) error {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
	}
	err := client.Create(context.TODO(), &ns)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func ensureNode(name string, client client.Client) error {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
	}
	err := client.Create(context.TODO(), &node)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

//nolint:interfacer
func isKubeService(svc metav1.Object) bool {
	// grumble grumble linters grumble grumble
	return svc.GetNamespace() == "default" && svc.GetName() == "kubernetes"
}
