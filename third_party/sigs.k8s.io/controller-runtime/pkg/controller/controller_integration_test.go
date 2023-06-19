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

package controller_test

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ = Describe("controller", func() {
	var reconciled chan reconcile.Request
	ctx := context.Background()

	BeforeEach(func() {
		reconciled = make(chan reconcile.Request)
		Expect(cfg).NotTo(BeNil())
	})

	Describe("controller", func() {
		// TODO(directxman12): write a whole suite of controller-client interaction tests

		It("should reconcile", func() {
			By("Creating the Manager")
			cm, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating the Controller")
			instance, err := controller.New("foo-controller", cm, controller.Options{
				Reconciler: reconcile.Func(
					func(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
						reconciled <- request
						return reconcile.Result{}, nil
					}),
			})
			Expect(err).NotTo(HaveOccurred())

			By("Watching Resources")
			err = instance.Watch(
				source.Kind(cm.GetCache(), &appsv1.ReplicaSet{}),
				handler.EnqueueRequestForOwner(cm.GetScheme(), cm.GetRESTMapper(), &appsv1.Deployment{}),
			)
			Expect(err).NotTo(HaveOccurred())

			err = instance.Watch(source.Kind(cm.GetCache(), &appsv1.Deployment{}), &handler.EnqueueRequestForObject{})
			Expect(err).NotTo(HaveOccurred())

			err = cm.GetClient().Get(ctx, types.NamespacedName{Name: "foo"}, &corev1.Namespace{})
			Expect(err).To(Equal(&cache.ErrCacheNotStarted{}))
			err = cm.GetClient().List(ctx, &corev1.NamespaceList{})
			Expect(err).To(Equal(&cache.ErrCacheNotStarted{}))

			By("Starting the Manager")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(cm.Start(ctx)).NotTo(HaveOccurred())
			}()

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deployment-name"},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
									SecurityContext: &corev1.SecurityContext{
										Privileged: truePtr(),
									},
								},
							},
						},
					},
				},
			}
			expectedReconcileRequest := reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: "default",
				Name:      "deployment-name",
			}}

			By("Invoking Reconciling for Create")
			deployment, err = clientset.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Invoking Reconciling for Update")
			newDeployment := deployment.DeepCopy()
			newDeployment.Labels = map[string]string{"foo": "bar"}
			_, err = clientset.AppsV1().Deployments("default").Update(ctx, newDeployment, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Invoking Reconciling for an OwnedObject when it is created")
			replicaset := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rs-name",
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
						}),
					},
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"foo": "bar"},
					},
					Template: deployment.Spec.Template,
				},
			}
			replicaset, err = clientset.AppsV1().ReplicaSets("default").Create(ctx, replicaset, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Invoking Reconciling for an OwnedObject when it is updated")
			newReplicaset := replicaset.DeepCopy()
			newReplicaset.Labels = map[string]string{"foo": "bar"}
			_, err = clientset.AppsV1().ReplicaSets("default").Update(ctx, newReplicaset, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Invoking Reconciling for an OwnedObject when it is deleted")
			err = clientset.AppsV1().ReplicaSets("default").Delete(ctx, replicaset.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Invoking Reconciling for Delete")
			err = clientset.AppsV1().Deployments("default").
				Delete(ctx, "deployment-name", metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(<-reconciled).To(Equal(expectedReconcileRequest))

			By("Listing a type with a slice of pointers as items field")
			err = cm.GetClient().
				List(context.Background(), &controllertest.UnconventionalListTypeList{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func truePtr() *bool {
	t := true
	return &t
}
