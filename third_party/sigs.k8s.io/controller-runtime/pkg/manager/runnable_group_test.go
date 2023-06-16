package manager

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var _ = Describe("runnables", func() {
	errCh := make(chan error)

	It("should be able to create a new runnables object", func() {
		Expect(newRunnables(defaultBaseContext, errCh)).ToNot(BeNil())
	})

	It("should add caches to the appropriate group", func() {
		cache := &cacheProvider{cache: &informertest.FakeInformers{Error: fmt.Errorf("expected error")}}
		r := newRunnables(defaultBaseContext, errCh)
		Expect(r.Add(cache)).To(Succeed())
		Expect(r.Caches.startQueue).To(HaveLen(1))
	})

	It("should add webhooks to the appropriate group", func() {
		webhook := webhook.NewServer(webhook.Options{})
		r := newRunnables(defaultBaseContext, errCh)
		Expect(r.Add(webhook)).To(Succeed())
		Expect(r.Webhooks.startQueue).To(HaveLen(1))
	})

	It("should add any runnable to the leader election group", func() {
		err := errors.New("runnable func")
		runnable := RunnableFunc(func(c context.Context) error {
			return err
		})

		r := newRunnables(defaultBaseContext, errCh)
		Expect(r.Add(runnable)).To(Succeed())
		Expect(r.LeaderElection.startQueue).To(HaveLen(1))
	})
})

var _ = Describe("runnableGroup", func() {
	errCh := make(chan error)

	It("should be able to add new runnables before it starts", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rg := newRunnableGroup(defaultBaseContext, errCh)
		Expect(rg.Add(RunnableFunc(func(c context.Context) error {
			<-ctx.Done()
			return nil
		}), nil)).To(Succeed())

		Expect(rg.Started()).To(BeFalse())
	})

	It("should be able to add new runnables before and after start", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rg := newRunnableGroup(defaultBaseContext, errCh)
		Expect(rg.Add(RunnableFunc(func(c context.Context) error {
			<-ctx.Done()
			return nil
		}), nil)).To(Succeed())
		Expect(rg.Start(ctx)).To(Succeed())
		Expect(rg.Started()).To(BeTrue())
		Expect(rg.Add(RunnableFunc(func(c context.Context) error {
			<-ctx.Done()
			return nil
		}), nil)).To(Succeed())
	})

	It("should be able to add new runnables before and after start concurrently", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rg := newRunnableGroup(defaultBaseContext, errCh)

		go func() {
			defer GinkgoRecover()
			<-time.After(50 * time.Millisecond)
			Expect(rg.Start(ctx)).To(Succeed())
		}()

		for i := 0; i < 20; i++ {
			go func(i int) {
				defer GinkgoRecover()

				<-time.After(time.Duration(i) * 10 * time.Millisecond)
				Expect(rg.Add(RunnableFunc(func(c context.Context) error {
					<-ctx.Done()
					return nil
				}), nil)).To(Succeed())
			}(i)
		}
	})

	It("should be able to close the group and wait for all runnables to finish", func() {
		ctx, cancel := context.WithCancel(context.Background())

		exited := pointer.Int64(0)
		rg := newRunnableGroup(defaultBaseContext, errCh)
		for i := 0; i < 10; i++ {
			Expect(rg.Add(RunnableFunc(func(c context.Context) error {
				defer atomic.AddInt64(exited, 1)
				<-ctx.Done()
				<-time.After(time.Duration(i) * 10 * time.Millisecond)
				return nil
			}), nil)).To(Succeed())
		}
		Expect(rg.Start(ctx)).To(Succeed())

		// Cancel the context, asking the runnables to exit.
		cancel()
		rg.StopAndWait(context.Background())

		Expect(rg.Add(RunnableFunc(func(c context.Context) error {
			return nil
		}), nil)).ToNot(Succeed())

		Expect(atomic.LoadInt64(exited)).To(BeNumerically("==", 10))
	})

	It("should be able to wait for all runnables to be ready at different intervals", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		rg := newRunnableGroup(defaultBaseContext, errCh)

		go func() {
			defer GinkgoRecover()
			<-time.After(50 * time.Millisecond)
			Expect(rg.Start(ctx)).To(Succeed())
		}()

		for i := 0; i < 20; i++ {
			go func(i int) {
				defer GinkgoRecover()

				Expect(rg.Add(RunnableFunc(func(c context.Context) error {
					<-ctx.Done()
					return nil
				}), func(_ context.Context) bool {
					<-time.After(time.Duration(i) * 10 * time.Millisecond)
					return true
				})).To(Succeed())
			}(i)
		}
	})

	It("should not turn ready if some readiness check fail", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rg := newRunnableGroup(defaultBaseContext, errCh)

		go func() {
			defer GinkgoRecover()
			<-time.After(50 * time.Millisecond)
			Expect(rg.Start(ctx)).To(Succeed())
		}()

		for i := 0; i < 20; i++ {
			go func(i int) {
				defer GinkgoRecover()

				Expect(rg.Add(RunnableFunc(func(c context.Context) error {
					<-ctx.Done()
					return nil
				}), func(_ context.Context) bool {
					<-time.After(time.Duration(i) * 10 * time.Millisecond)
					return i%2 == 0 // Return false readiness all uneven indexes.
				})).To(Succeed())
			}(i)
		}
	})
})
