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

package source_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

var _ = Describe("Source", func() {
	Describe("Kind", func() {
		var c chan struct{}
		var p *corev1.Pod
		var ic *informertest.FakeInformers

		BeforeEach(func() {
			ic = &informertest.FakeInformers{}
			c = make(chan struct{})
			p = &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "test"},
					},
				},
			}
		})

		Context("for a Pod resource", func() {
			It("should provide a Pod CreateEvent", func() {
				c := make(chan struct{})
				p := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test", Image: "test"},
						},
					},
				}

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := source.Kind(ic, &corev1.Pod{})
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(ctx context.Context, evt event.CreateEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Expect(q2).To(Equal(q))
						Expect(evt.Object).To(Equal(p))
						close(c)
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(context.Context, event.GenericEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected GenericEvent")
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())
				Expect(instance.WaitForSync(context.Background())).NotTo(HaveOccurred())

				i, err := ic.FakeInformerFor(&corev1.Pod{})
				Expect(err).NotTo(HaveOccurred())

				i.Add(p)
				<-c
			})

			It("should provide a Pod UpdateEvent", func() {
				p2 := p.DeepCopy()
				p2.SetLabels(map[string]string{"biz": "baz"})

				ic := &informertest.FakeInformers{}
				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := source.Kind(ic, &corev1.Pod{})
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(ctx context.Context, evt event.CreateEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(ctx context.Context, evt event.UpdateEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Expect(q2).To(BeIdenticalTo(q))
						Expect(evt.ObjectOld).To(Equal(p))

						Expect(evt.ObjectNew).To(Equal(p2))

						close(c)
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(context.Context, event.GenericEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected GenericEvent")
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())
				Expect(instance.WaitForSync(context.Background())).NotTo(HaveOccurred())

				i, err := ic.FakeInformerFor(&corev1.Pod{})
				Expect(err).NotTo(HaveOccurred())

				i.Update(p, p2)
				<-c
			})

			It("should provide a Pod DeletedEvent", func() {
				c := make(chan struct{})
				p := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "test", Image: "test"},
						},
					},
				}

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := source.Kind(ic, &corev1.Pod{})
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(ctx context.Context, evt event.DeleteEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Expect(q2).To(BeIdenticalTo(q))
						Expect(evt.Object).To(Equal(p))
						close(c)
					},
					GenericFunc: func(context.Context, event.GenericEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected GenericEvent")
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())
				Expect(instance.WaitForSync(context.Background())).NotTo(HaveOccurred())

				i, err := ic.FakeInformerFor(&corev1.Pod{})
				Expect(err).NotTo(HaveOccurred())

				i.Delete(p)
				<-c
			})
		})

		It("should return an error from Start cache was not provided", func() {
			instance := source.Kind(nil, &corev1.Pod{})
			err := instance.Start(ctx, nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must create Kind with a non-nil cache"))
		})

		It("should return an error from Start if a type was not provided", func() {
			instance := source.Kind(ic, nil)
			err := instance.Start(ctx, nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must create Kind with a non-nil object"))
		})

		It("should return an error if syncing fails", func() {
			f := false
			instance := source.Kind(&informertest.FakeInformers{Synced: &f}, &corev1.Pod{})
			Expect(instance.Start(context.Background(), nil, nil)).NotTo(HaveOccurred())
			err := instance.WaitForSync(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("cache did not sync"))

		})

		Context("for a Kind not in the cache", func() {
			It("should return an error when WaitForSync is called", func() {
				ic.Error = fmt.Errorf("test error")
				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")

				ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				instance := source.Kind(ic, &corev1.Pod{})
				err := instance.Start(ctx, handler.Funcs{}, q)
				Expect(err).NotTo(HaveOccurred())
				Eventually(instance.WaitForSync(context.Background())).Should(HaveOccurred())
			})
		})

		It("should return an error if syncing fails", func() {
			f := false
			instance := source.Kind(&informertest.FakeInformers{Synced: &f}, &corev1.Pod{})
			Expect(instance.Start(context.Background(), nil, nil)).NotTo(HaveOccurred())
			err := instance.WaitForSync(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("cache did not sync"))

		})
	})

	Describe("Func", func() {
		It("should be called from Start", func() {
			run := false
			instance := source.Func(func(
				context.Context,
				handler.EventHandler,
				workqueue.RateLimitingInterface, ...predicate.Predicate) error {
				run = true
				return nil
			})
			Expect(instance.Start(ctx, nil, nil)).NotTo(HaveOccurred())
			Expect(run).To(BeTrue())

			expected := fmt.Errorf("expected error: Func")
			instance = source.Func(func(
				context.Context,
				handler.EventHandler,
				workqueue.RateLimitingInterface, ...predicate.Predicate) error {
				return expected
			})
			Expect(instance.Start(ctx, nil, nil)).To(Equal(expected))
		})
	})

	Describe("Channel", func() {
		var ctx context.Context
		var cancel context.CancelFunc
		var ch chan event.GenericEvent

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			ch = make(chan event.GenericEvent)
		})

		AfterEach(func() {
			cancel()
			close(ch)
		})

		Context("for a source", func() {
			It("should provide a GenericEvent", func() {
				ch := make(chan event.GenericEvent)
				c := make(chan struct{})
				p := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
				}
				evt := event.GenericEvent{
					Object: p,
				}
				// Event that should be filtered out by predicates
				invalidEvt := event.GenericEvent{}

				// Predicate to filter out empty event
				prct := predicate.Funcs{
					GenericFunc: func(e event.GenericEvent) bool {
						return e.Object != nil
					},
				}

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := &source.Channel{Source: ch}
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						// The empty event should have been filtered out by the predicates,
						// and will not be passed to the handler.
						Expect(q2).To(BeIdenticalTo(q))
						Expect(evt.Object).To(Equal(p))
						close(c)
					},
				}, q, prct)
				Expect(err).NotTo(HaveOccurred())

				ch <- invalidEvt
				ch <- evt
				<-c
			})
			It("should get pending events processed once channel unblocked", func() {
				ch := make(chan event.GenericEvent)
				unblock := make(chan struct{})
				processed := make(chan struct{})
				evt := event.GenericEvent{}
				eventCount := 0

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				// Add a handler to get distribution blocked
				instance := &source.Channel{Source: ch}
				instance.DestBufferSize = 1
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						// Block for the first time
						if eventCount == 0 {
							<-unblock
						}
						eventCount++

						if eventCount == 3 {
							close(processed)
						}
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())

				// Write 3 events into the source channel.
				// The 1st should be passed into the generic func of the handler;
				// The 2nd should be fetched out of the source channel, and waiting to write into dest channel;
				// The 3rd should be pending in the source channel.
				ch <- evt
				ch <- evt
				ch <- evt

				// Validate none of the events have been processed.
				Expect(eventCount).To(Equal(0))

				close(unblock)

				<-processed

				// Validate all of the events have been processed.
				Expect(eventCount).To(Equal(3))
			})
			It("should be able to cope with events in the channel before the source is started", func() {
				ch := make(chan event.GenericEvent, 1)
				processed := make(chan struct{})
				evt := event.GenericEvent{}
				ch <- evt

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				// Add a handler to get distribution blocked
				instance := &source.Channel{Source: ch}
				instance.DestBufferSize = 1

				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()

						close(processed)
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())

				<-processed
			})
			It("should stop when the source channel is closed", func() {
				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				// if we didn't stop, we'd start spamming the queue with empty
				// messages as we "received" a zero-valued GenericEvent from
				// the source channel

				By("creating a channel with one element, then closing it")
				ch := make(chan event.GenericEvent, 1)
				evt := event.GenericEvent{}
				ch <- evt
				close(ch)

				By("feeding that channel to a channel source")
				src := &source.Channel{Source: ch}

				processed := make(chan struct{})
				defer close(processed)

				err := src.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()

						processed <- struct{}{}
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())

				By("expecting to only get one event")
				Eventually(processed).Should(Receive())
				Consistently(processed).ShouldNot(Receive())
			})
			It("should get error if no source specified", func() {
				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := &source.Channel{ /*no source specified*/ }
				err := instance.Start(ctx, handler.Funcs{}, q)
				Expect(err).To(Equal(fmt.Errorf("must specify Channel.Source")))
			})
		})
		Context("for multi sources (handlers)", func() {
			It("should provide GenericEvents for all handlers", func() {
				ch := make(chan event.GenericEvent)
				p := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"},
				}
				evt := event.GenericEvent{
					Object: p,
				}

				var resEvent1, resEvent2 event.GenericEvent
				c1 := make(chan struct{})
				c2 := make(chan struct{})

				q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "test")
				instance := &source.Channel{Source: ch}
				err := instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Expect(q2).To(BeIdenticalTo(q))
						Expect(evt.Object).To(Equal(p))
						resEvent1 = evt
						close(c1)
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())

				err = instance.Start(ctx, handler.Funcs{
					CreateFunc: func(context.Context, event.CreateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected CreateEvent")
					},
					UpdateFunc: func(context.Context, event.UpdateEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected UpdateEvent")
					},
					DeleteFunc: func(context.Context, event.DeleteEvent, workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Fail("Unexpected DeleteEvent")
					},
					GenericFunc: func(ctx context.Context, evt event.GenericEvent, q2 workqueue.RateLimitingInterface) {
						defer GinkgoRecover()
						Expect(q2).To(BeIdenticalTo(q))
						Expect(evt.Object).To(Equal(p))
						resEvent2 = evt
						close(c2)
					},
				}, q)
				Expect(err).NotTo(HaveOccurred())

				ch <- evt
				<-c1
				<-c2

				// Validate the two handlers received same event
				Expect(resEvent1).To(Equal(resEvent2))
			})
		})
	})
})
