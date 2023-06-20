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

package reconcile_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("reconcile", func() {
	Describe("Result", func() {
		It("IsZero should return true if empty", func() {
			var res *reconcile.Result
			Expect(res.IsZero()).To(BeTrue())
			res2 := &reconcile.Result{}
			Expect(res2.IsZero()).To(BeTrue())
			res3 := reconcile.Result{}
			Expect(res3.IsZero()).To(BeTrue())
		})

		It("IsZero should return false if Requeue is set to true", func() {
			res := reconcile.Result{Requeue: true}
			Expect(res.IsZero()).To(BeFalse())
		})

		It("IsZero should return false if RequeueAfter is set to true", func() {
			res := reconcile.Result{RequeueAfter: 1 * time.Second}
			Expect(res.IsZero()).To(BeFalse())
		})
	})

	Describe("Func", func() {
		It("should call the function with the request and return a nil error.", func() {
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "foo", Namespace: "bar"},
			}
			result := reconcile.Result{
				Requeue: true,
			}

			instance := reconcile.Func(func(_ context.Context, r reconcile.Request) (reconcile.Result, error) {
				defer GinkgoRecover()
				Expect(r).To(Equal(request))

				return result, nil
			})
			actualResult, actualErr := instance.Reconcile(context.Background(), request)
			Expect(actualResult).To(Equal(result))
			Expect(actualErr).NotTo(HaveOccurred())
		})

		It("should call the function with the request and return an error.", func() {
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "foo", Namespace: "bar"},
			}
			result := reconcile.Result{
				Requeue: false,
			}
			err := fmt.Errorf("hello world")

			instance := reconcile.Func(func(_ context.Context, r reconcile.Request) (reconcile.Result, error) {
				defer GinkgoRecover()
				Expect(r).To(Equal(request))

				return result, err
			})
			actualResult, actualErr := instance.Reconcile(context.Background(), request)
			Expect(actualResult).To(Equal(result))
			Expect(actualErr).To(Equal(err))
		})

		It("should allow unwrapping inner error from terminal error", func() {
			inner := apierrors.NewGone("")
			terminalError := reconcile.TerminalError(inner)

			Expect(apierrors.IsGone(terminalError)).To(BeTrue())
		})
	})
})
