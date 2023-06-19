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

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/internal/controller/metrics"
	"sigs.k8s.io/controller-runtime/pkg/internal/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var _ = Describe("controller", func() {
	var fakeReconcile *fakeReconciler
	var ctrl *Controller
	var queue *controllertest.Queue
	var reconciled chan reconcile.Request
	var request = reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: "foo", Name: "bar"},
	}

	BeforeEach(func() {
		reconciled = make(chan reconcile.Request)
		fakeReconcile = &fakeReconciler{
			Requests: reconciled,
			results:  make(chan fakeReconcileResultPair, 10 /* chosen by the completely scientific approach of guessing */),
		}
		queue = &controllertest.Queue{
			Interface: workqueue.New(),
		}
		ctrl = &Controller{
			MaxConcurrentReconciles: 1,
			Do:                      fakeReconcile,
			MakeQueue:               func() workqueue.RateLimitingInterface { return queue },
			LogConstructor: func(_ *reconcile.Request) logr.Logger {
				return log.RuntimeLog.WithName("controller").WithName("test")
			},
		}
	})

	Describe("Reconciler", func() {
		It("should call the Reconciler function", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ctrl.Do = reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
				return reconcile.Result{Requeue: true}, nil
			})
			result, err := ctrl.Reconcile(ctx,
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "foo", Name: "bar"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true}))
		})

		It("should not recover panic if RecoverPanic is false by default", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			defer func() {
				Expect(recover()).ShouldNot(BeNil())
			}()
			ctrl.Do = reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
				var res *reconcile.Result
				return *res, nil
			})
			_, _ = ctrl.Reconcile(ctx,
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "foo", Name: "bar"}})
		})

		It("should recover panic if RecoverPanic is true", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			defer func() {
				Expect(recover()).To(BeNil())
			}()
			ctrl.RecoverPanic = pointer.Bool(true)
			ctrl.Do = reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
				var res *reconcile.Result
				return *res, nil
			})
			_, err := ctrl.Reconcile(ctx,
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "foo", Name: "bar"}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("[recovered]"))
		})
	})

	Describe("Start", func() {
		It("should return an error if there is an error waiting for the informers", func() {
			f := false
			ctrl.startWatches = []watchDescription{{
				src: source.Kind(&informertest.FakeInformers{Synced: &f}, &corev1.Pod{}),
			}}
			ctrl.Name = "foo"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := ctrl.Start(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to wait for foo caches to sync"))
		})

		It("should error when cache sync timeout occurs", func() {
			ctrl.CacheSyncTimeout = 10 * time.Nanosecond

			c, err := cache.New(cfg, cache.Options{})
			Expect(err).NotTo(HaveOccurred())
			c = &cacheWithIndefinitelyBlockingGetInformer{c}

			ctrl.startWatches = []watchDescription{{
				src: source.Kind(c, &appsv1.Deployment{}),
			}}
			ctrl.Name = "testcontroller"

			err = ctrl.Start(context.TODO())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to wait for testcontroller caches to sync: timed out waiting for cache to be synced"))
		})

		It("should not error when context cancelled", func() {
			ctrl.CacheSyncTimeout = 1 * time.Second

			sourceSynced := make(chan struct{})
			c, err := cache.New(cfg, cache.Options{})
			Expect(err).NotTo(HaveOccurred())
			c = &cacheWithIndefinitelyBlockingGetInformer{c}
			ctrl.startWatches = []watchDescription{{
				src: &singnallingSourceWrapper{
					SyncingSource: source.Kind(c, &appsv1.Deployment{}),
					cacheSyncDone: sourceSynced,
				},
			}}
			ctrl.Name = "testcontroller"

			ctx, cancel := context.WithCancel(context.TODO())
			go func() {
				defer GinkgoRecover()
				err = ctrl.Start(ctx)
				Expect(err).To(Succeed())
			}()

			cancel()
			<-sourceSynced
		})

		It("should not error when cache sync timeout is of sufficiently high", func() {
			ctrl.CacheSyncTimeout = 1 * time.Second

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sourceSynced := make(chan struct{})
			c, err := cache.New(cfg, cache.Options{})
			Expect(err).NotTo(HaveOccurred())
			ctrl.startWatches = []watchDescription{{
				src: &singnallingSourceWrapper{
					SyncingSource: source.Kind(c, &appsv1.Deployment{}),
					cacheSyncDone: sourceSynced,
				},
			}}

			go func() {
				defer GinkgoRecover()
				Expect(c.Start(ctx)).To(Succeed())
			}()

			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).To(Succeed())
			}()

			<-sourceSynced
		})

		It("should process events from source.Channel", func() {
			// channel to be closed when event is processed
			processed := make(chan struct{})
			// source channel
			ch := make(chan event.GenericEvent, 1)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			// event to be sent to the channel
			p := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
			}
			evt := event.GenericEvent{
				Object: p,
			}

			ins := &source.Channel{Source: ch}
			ins.DestBufferSize = 1

			// send the event to the channel
			ch <- evt

			ctrl.startWatches = []watchDescription{{
				src: ins,
				handler: handler.Funcs{
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						close(processed)
					},
				},
			}}

			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).To(Succeed())
			}()
			<-processed
		})

		It("should error when channel source is not specified", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ins := &source.Channel{}
			ctrl.startWatches = []watchDescription{{
				src: ins,
			}}

			e := ctrl.Start(ctx)
			Expect(e).NotTo(BeNil())
			Expect(e.Error()).To(ContainSubstring("must specify Channel.Source"))
		})

		It("should call Start on sources with the appropriate EventHandler, Queue, and Predicates", func() {
			pr1 := &predicate.Funcs{}
			pr2 := &predicate.Funcs{}
			evthdl := &handler.EnqueueRequestForObject{}
			started := false
			src := source.Func(func(ctx context.Context, e handler.EventHandler, q workqueue.RateLimitingInterface, p ...predicate.Predicate) error {
				defer GinkgoRecover()
				Expect(e).To(Equal(evthdl))
				Expect(q).To(Equal(ctrl.Queue))
				Expect(p).To(ConsistOf(pr1, pr2))

				started = true
				return nil
			})
			Expect(ctrl.Watch(src, evthdl, pr1, pr2)).NotTo(HaveOccurred())

			// Use a cancelled context so Start doesn't block
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(ctrl.Start(ctx)).To(Succeed())
			Expect(started).To(BeTrue())
		})

		It("should return an error if there is an error starting sources", func() {
			err := fmt.Errorf("Expected Error: could not start source")
			src := source.Func(func(context.Context, handler.EventHandler,
				workqueue.RateLimitingInterface,
				...predicate.Predicate) error {
				defer GinkgoRecover()
				return err
			})
			Expect(ctrl.Watch(src, &handler.EnqueueRequestForObject{})).To(Succeed())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			Expect(ctrl.Start(ctx)).To(Equal(err))
		})

		It("should return an error if it gets started more than once", func() {
			// Use a cancelled context so Start doesn't block
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(ctrl.Start(ctx)).To(BeNil())
			err := ctrl.Start(ctx)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(Equal("controller was started more than once. This is likely to be caused by being added to a manager multiple times"))
		})

	})

	Describe("Processing queue items from a Controller", func() {
		It("should call Reconciler if an item is enqueued", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()
			queue.Add(request)

			By("Invoking Reconciler")
			fakeReconcile.AddResult(reconcile.Result{}, nil)
			Expect(<-reconciled).To(Equal(request))

			By("Removing the item from the queue")
			Eventually(queue.Len).Should(Equal(0))
			Eventually(func() int { return queue.NumRequeues(request) }).Should(Equal(0))
		})

		It("should continue to process additional queue items after the first", func() {
			ctrl.Do = reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
				defer GinkgoRecover()
				Fail("Reconciler should not have been called")
				return reconcile.Result{}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			By("adding two bad items to the queue")
			queue.Add("foo/bar1")
			queue.Add("foo/bar2")

			By("expecting both of them to be skipped")
			Eventually(queue.Len).Should(Equal(0))
			Eventually(func() int { return queue.NumRequeues(request) }).Should(Equal(0))
		})

		PIt("should forget an item if it is not a Request and continue processing items", func() {
			// TODO(community): write this test
		})

		It("should requeue a Request if there is an error and continue processing items", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			queue.Add(request)

			By("Invoking Reconciler which will give an error")
			fakeReconcile.AddResult(reconcile.Result{}, fmt.Errorf("expected error: reconcile"))
			Expect(<-reconciled).To(Equal(request))
			queue.AddedRateLimitedLock.Lock()
			Expect(queue.AddedRatelimited).To(Equal([]any{request}))
			queue.AddedRateLimitedLock.Unlock()

			By("Invoking Reconciler a second time without error")
			fakeReconcile.AddResult(reconcile.Result{}, nil)
			Expect(<-reconciled).To(Equal(request))

			By("Removing the item from the queue")
			Eventually(queue.Len).Should(Equal(0))
			Eventually(func() int { return queue.NumRequeues(request) }, 1.0).Should(Equal(0))
		})

		It("should not requeue a Request if there is a terminal error", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			queue.Add(request)

			By("Invoking Reconciler which will give an error")
			fakeReconcile.AddResult(reconcile.Result{}, reconcile.TerminalError(fmt.Errorf("expected error: reconcile")))
			Expect(<-reconciled).To(Equal(request))

			queue.AddedRateLimitedLock.Lock()
			Expect(queue.AddedRatelimited).To(BeEmpty())
			queue.AddedRateLimitedLock.Unlock()

			Expect(queue.Len()).Should(Equal(0))
		})

		// TODO(directxman12): we should ensure that backoff occurrs with error requeue

		It("should not reset backoff until there's a non-error result", func() {
			dq := &DelegatingQueue{RateLimitingInterface: ctrl.MakeQueue()}
			ctrl.MakeQueue = func() workqueue.RateLimitingInterface { return dq }

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			dq.Add(request)
			Expect(dq.getCounts()).To(Equal(countInfo{Trying: 1}))

			By("Invoking Reconciler which returns an error")
			fakeReconcile.AddResult(reconcile.Result{}, fmt.Errorf("something's wrong"))
			Expect(<-reconciled).To(Equal(request))
			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 1, AddRateLimited: 1}))

			By("Invoking Reconciler a second time with an error")
			fakeReconcile.AddResult(reconcile.Result{}, fmt.Errorf("another thing's wrong"))
			Expect(<-reconciled).To(Equal(request))

			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 1, AddRateLimited: 2}))

			By("Invoking Reconciler a third time, where it finally does not return an error")
			fakeReconcile.AddResult(reconcile.Result{}, nil)
			Expect(<-reconciled).To(Equal(request))

			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 0, AddRateLimited: 2}))

			By("Removing the item from the queue")
			Eventually(dq.Len).Should(Equal(0))
			Eventually(func() int { return dq.NumRequeues(request) }).Should(Equal(0))
		})

		It("should requeue a Request with rate limiting if the Result sets Requeue:true and continue processing items", func() {
			dq := &DelegatingQueue{RateLimitingInterface: ctrl.MakeQueue()}
			ctrl.MakeQueue = func() workqueue.RateLimitingInterface { return dq }

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			dq.Add(request)
			Expect(dq.getCounts()).To(Equal(countInfo{Trying: 1}))

			By("Invoking Reconciler which will ask for requeue")
			fakeReconcile.AddResult(reconcile.Result{Requeue: true}, nil)
			Expect(<-reconciled).To(Equal(request))
			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 1, AddRateLimited: 1}))

			By("Invoking Reconciler a second time without asking for requeue")
			fakeReconcile.AddResult(reconcile.Result{Requeue: false}, nil)
			Expect(<-reconciled).To(Equal(request))

			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 0, AddRateLimited: 1}))

			By("Removing the item from the queue")
			Eventually(dq.Len).Should(Equal(0))
			Eventually(func() int { return dq.NumRequeues(request) }).Should(Equal(0))
		})

		It("should requeue a Request after a duration (but not rate-limitted) if the Result sets RequeueAfter (regardless of Requeue)", func() {
			dq := &DelegatingQueue{RateLimitingInterface: ctrl.MakeQueue()}
			ctrl.MakeQueue = func() workqueue.RateLimitingInterface { return dq }

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			dq.Add(request)
			Expect(dq.getCounts()).To(Equal(countInfo{Trying: 1}))

			By("Invoking Reconciler which will ask for requeue & requeueafter")
			fakeReconcile.AddResult(reconcile.Result{RequeueAfter: time.Millisecond * 100, Requeue: true}, nil)
			Expect(<-reconciled).To(Equal(request))
			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 0, AddAfter: 1}))

			By("Invoking Reconciler a second time asking for a requeueafter only")
			fakeReconcile.AddResult(reconcile.Result{RequeueAfter: time.Millisecond * 100}, nil)
			Expect(<-reconciled).To(Equal(request))

			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: -1 /* we don't increment the count in addafter */, AddAfter: 2}))

			By("Removing the item from the queue")
			Eventually(dq.Len).Should(Equal(0))
			Eventually(func() int { return dq.NumRequeues(request) }).Should(Equal(0))
		})

		It("should perform error behavior if error is not nil, regardless of RequeueAfter", func() {
			dq := &DelegatingQueue{RateLimitingInterface: ctrl.MakeQueue()}
			ctrl.MakeQueue = func() workqueue.RateLimitingInterface { return dq }

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
			}()

			dq.Add(request)
			Expect(dq.getCounts()).To(Equal(countInfo{Trying: 1}))

			By("Invoking Reconciler which will ask for requeueafter with an error")
			fakeReconcile.AddResult(reconcile.Result{RequeueAfter: time.Millisecond * 100}, fmt.Errorf("expected error: reconcile"))
			Expect(<-reconciled).To(Equal(request))
			Eventually(dq.getCounts).Should(Equal(countInfo{Trying: 1, AddRateLimited: 1}))

			By("Invoking Reconciler a second time asking for requeueafter without errors")
			fakeReconcile.AddResult(reconcile.Result{RequeueAfter: time.Millisecond * 100}, nil)
			Expect(<-reconciled).To(Equal(request))
			Eventually(dq.getCounts).Should(Equal(countInfo{AddAfter: 1, AddRateLimited: 1}))

			By("Removing the item from the queue")
			Eventually(dq.Len).Should(Equal(0))
			Eventually(func() int { return dq.NumRequeues(request) }).Should(Equal(0))
		})

		PIt("should return if the queue is shutdown", func() {
			// TODO(community): write this test
		})

		PIt("should wait for informers to be synced before processing items", func() {
			// TODO(community): write this test
		})

		PIt("should create a new go routine for MaxConcurrentReconciles", func() {
			// TODO(community): write this test
		})

		Context("prometheus metric reconcile_total", func() {
			var reconcileTotal dto.Metric

			BeforeEach(func() {
				ctrlmetrics.ReconcileTotal.Reset()
				reconcileTotal.Reset()
			})

			It("should get updated on successful reconciliation", func() {
				Expect(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "success").Write(&reconcileTotal)).To(Succeed())
					if reconcileTotal.GetCounter().GetValue() != 0.0 {
						return fmt.Errorf("metric reconcile total not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()
				By("Invoking Reconciler which will succeed")
				queue.Add(request)

				fakeReconcile.AddResult(reconcile.Result{}, nil)
				Expect(<-reconciled).To(Equal(request))
				Eventually(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "success").Write(&reconcileTotal)).To(Succeed())
					if actual := reconcileTotal.GetCounter().GetValue(); actual != 1.0 {
						return fmt.Errorf("metric reconcile total expected: %v and got: %v", 1.0, actual)
					}
					return nil
				}, 2.0).Should(Succeed())
			})

			It("should get updated on reconcile errors", func() {
				Expect(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "error").Write(&reconcileTotal)).To(Succeed())
					if reconcileTotal.GetCounter().GetValue() != 0.0 {
						return fmt.Errorf("metric reconcile total not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()
				By("Invoking Reconciler which will give an error")
				queue.Add(request)

				fakeReconcile.AddResult(reconcile.Result{}, fmt.Errorf("expected error: reconcile"))
				Expect(<-reconciled).To(Equal(request))
				Eventually(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "error").Write(&reconcileTotal)).To(Succeed())
					if actual := reconcileTotal.GetCounter().GetValue(); actual != 1.0 {
						return fmt.Errorf("metric reconcile total expected: %v and got: %v", 1.0, actual)
					}
					return nil
				}, 2.0).Should(Succeed())
			})

			It("should get updated when reconcile returns with retry enabled", func() {
				Expect(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "retry").Write(&reconcileTotal)).To(Succeed())
					if reconcileTotal.GetCounter().GetValue() != 0.0 {
						return fmt.Errorf("metric reconcile total not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()

				By("Invoking Reconciler which will return result with Requeue enabled")
				queue.Add(request)

				fakeReconcile.AddResult(reconcile.Result{Requeue: true}, nil)
				Expect(<-reconciled).To(Equal(request))
				Eventually(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "requeue").Write(&reconcileTotal)).To(Succeed())
					if actual := reconcileTotal.GetCounter().GetValue(); actual != 1.0 {
						return fmt.Errorf("metric reconcile total expected: %v and got: %v", 1.0, actual)
					}
					return nil
				}, 2.0).Should(Succeed())
			})

			It("should get updated when reconcile returns with retryAfter enabled", func() {
				Expect(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "retry_after").Write(&reconcileTotal)).To(Succeed())
					if reconcileTotal.GetCounter().GetValue() != 0.0 {
						return fmt.Errorf("metric reconcile total not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()
				By("Invoking Reconciler which will return result with requeueAfter enabled")
				queue.Add(request)

				fakeReconcile.AddResult(reconcile.Result{RequeueAfter: 5 * time.Hour}, nil)
				Expect(<-reconciled).To(Equal(request))
				Eventually(func() error {
					Expect(ctrlmetrics.ReconcileTotal.WithLabelValues(ctrl.Name, "requeue_after").Write(&reconcileTotal)).To(Succeed())
					if actual := reconcileTotal.GetCounter().GetValue(); actual != 1.0 {
						return fmt.Errorf("metric reconcile total expected: %v and got: %v", 1.0, actual)
					}
					return nil
				}, 2.0).Should(Succeed())
			})
		})

		Context("should update prometheus metrics", func() {
			It("should requeue a Request if there is an error and continue processing items", func() {
				var reconcileErrs dto.Metric
				ctrlmetrics.ReconcileErrors.Reset()
				Expect(func() error {
					Expect(ctrlmetrics.ReconcileErrors.WithLabelValues(ctrl.Name).Write(&reconcileErrs)).To(Succeed())
					if reconcileErrs.GetCounter().GetValue() != 0.0 {
						return fmt.Errorf("metric reconcile errors not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()
				queue.Add(request)

				By("Invoking Reconciler which will give an error")
				fakeReconcile.AddResult(reconcile.Result{}, fmt.Errorf("expected error: reconcile"))
				Expect(<-reconciled).To(Equal(request))
				Eventually(func() error {
					Expect(ctrlmetrics.ReconcileErrors.WithLabelValues(ctrl.Name).Write(&reconcileErrs)).To(Succeed())
					if reconcileErrs.GetCounter().GetValue() != 1.0 {
						return fmt.Errorf("metrics not updated")
					}
					return nil
				}, 2.0).Should(Succeed())

				By("Invoking Reconciler a second time without error")
				fakeReconcile.AddResult(reconcile.Result{}, nil)
				Expect(<-reconciled).To(Equal(request))

				By("Removing the item from the queue")
				Eventually(queue.Len).Should(Equal(0))
				Eventually(func() int { return queue.NumRequeues(request) }).Should(Equal(0))
			})

			It("should add a reconcile time to the reconcile time histogram", func() {
				var reconcileTime dto.Metric
				ctrlmetrics.ReconcileTime.Reset()

				Expect(func() error {
					histObserver := ctrlmetrics.ReconcileTime.WithLabelValues(ctrl.Name)
					hist := histObserver.(prometheus.Histogram)
					Expect(hist.Write(&reconcileTime)).To(Succeed())
					if reconcileTime.GetHistogram().GetSampleCount() != uint64(0) {
						return fmt.Errorf("metrics not reset")
					}
					return nil
				}()).Should(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(ctrl.Start(ctx)).NotTo(HaveOccurred())
				}()
				queue.Add(request)

				By("Invoking Reconciler")
				fakeReconcile.AddResult(reconcile.Result{}, nil)
				Expect(<-reconciled).To(Equal(request))

				By("Removing the item from the queue")
				Eventually(queue.Len).Should(Equal(0))
				Eventually(func() int { return queue.NumRequeues(request) }).Should(Equal(0))

				Eventually(func() error {
					histObserver := ctrlmetrics.ReconcileTime.WithLabelValues(ctrl.Name)
					hist := histObserver.(prometheus.Histogram)
					Expect(hist.Write(&reconcileTime)).To(Succeed())
					if reconcileTime.GetHistogram().GetSampleCount() == uint64(0) {
						return fmt.Errorf("metrics not updated")
					}
					return nil
				}, 2.0).Should(Succeed())
			})
		})
	})
})

var _ = Describe("ReconcileIDFromContext function", func() {
	It("should return an empty string if there is nothing in the context", func() {
		ctx := context.Background()
		reconcileID := ReconcileIDFromContext(ctx)

		Expect(reconcileID).To(Equal(types.UID("")))
	})

	It("should return the correct reconcileID from context", func() {
		const expectedReconcileID = types.UID("uuid")
		ctx := addReconcileID(context.Background(), expectedReconcileID)
		reconcileID := ReconcileIDFromContext(ctx)

		Expect(reconcileID).To(Equal(expectedReconcileID))
	})
})

type DelegatingQueue struct {
	workqueue.RateLimitingInterface
	mu sync.Mutex

	countAddRateLimited int
	countAdd            int
	countAddAfter       int
}

func (q *DelegatingQueue) AddRateLimited(item interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.countAddRateLimited++
	q.RateLimitingInterface.AddRateLimited(item)
}

func (q *DelegatingQueue) AddAfter(item interface{}, d time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.countAddAfter++
	q.RateLimitingInterface.AddAfter(item, d)
}

func (q *DelegatingQueue) Add(item interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.countAdd++

	q.RateLimitingInterface.Add(item)
}

func (q *DelegatingQueue) Forget(item interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.countAdd--

	q.RateLimitingInterface.Forget(item)
}

type countInfo struct {
	Trying, AddAfter, AddRateLimited int
}

func (q *DelegatingQueue) getCounts() countInfo {
	q.mu.Lock()
	defer q.mu.Unlock()

	return countInfo{
		Trying:         q.countAdd,
		AddAfter:       q.countAddAfter,
		AddRateLimited: q.countAddRateLimited,
	}
}

type fakeReconcileResultPair struct {
	Result reconcile.Result
	Err    error
}

type fakeReconciler struct {
	Requests chan reconcile.Request
	results  chan fakeReconcileResultPair
}

func (f *fakeReconciler) AddResult(res reconcile.Result, err error) {
	f.results <- fakeReconcileResultPair{Result: res, Err: err}
}

func (f *fakeReconciler) Reconcile(_ context.Context, r reconcile.Request) (reconcile.Result, error) {
	res := <-f.results
	if f.Requests != nil {
		f.Requests <- r
	}
	return res.Result, res.Err
}

type singnallingSourceWrapper struct {
	cacheSyncDone chan struct{}
	source.SyncingSource
}

func (s *singnallingSourceWrapper) WaitForSync(ctx context.Context) error {
	defer func() {
		close(s.cacheSyncDone)
	}()
	return s.SyncingSource.WaitForSync(ctx)
}

var _ cache.Cache = &cacheWithIndefinitelyBlockingGetInformer{}

// cacheWithIndefinitelyBlockingGetInformer has a GetInformer implementation that blocks indefinitely or until its
// context is cancelled.
// We need it as a workaround for testenvs lack of support for a secure apiserver, because the insecure port always
// implies the allow all authorizer, so we can not simulate rbac issues with it. They are the usual cause of the real
// caches GetInformer blocking showing this behavior.
// TODO: Remove this once envtest supports a secure apiserver.
type cacheWithIndefinitelyBlockingGetInformer struct {
	cache.Cache
}

func (c *cacheWithIndefinitelyBlockingGetInformer) GetInformer(ctx context.Context, obj client.Object) (cache.Informer, error) {
	<-ctx.Done()
	return nil, errors.New("GetInformer timed out")
}
