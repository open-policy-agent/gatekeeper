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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/goleak"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	internalcontroller "sigs.k8s.io/controller-runtime/pkg/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var _ = Describe("controller.Controller", func() {
	rec := reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, nil
	})

	Describe("New", func() {
		It("should return an error if Name is not Specified", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())
			c, err := controller.New("", m, controller.Options{Reconciler: rec})
			Expect(c).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("must specify Name for Controller"))
		})

		It("should return an error if Reconciler is not Specified", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("foo", m, controller.Options{})
			Expect(c).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("must specify Reconciler"))
		})

		It("should not return an error if two controllers are registered with different names", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c1, err := controller.New("c1", m, controller.Options{Reconciler: rec})
			Expect(err).NotTo(HaveOccurred())
			Expect(c1).ToNot(BeNil())

			c2, err := controller.New("c2", m, controller.Options{Reconciler: rec})
			Expect(err).NotTo(HaveOccurred())
			Expect(c2).ToNot(BeNil())
		})

		It("should not leak goroutines when stopped", func() {
			currentGRs := goleak.IgnoreCurrent()

			ctx, cancel := context.WithCancel(context.Background())
			watchChan := make(chan event.GenericEvent, 1)
			watch := &source.Channel{Source: watchChan}
			watchChan <- event.GenericEvent{Object: &corev1.Pod{}}

			reconcileStarted := make(chan struct{})
			controllerFinished := make(chan struct{})
			rec := reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
				defer GinkgoRecover()
				close(reconcileStarted)
				// Make sure reconciliation takes a moment and is not quicker than the controllers
				// shutdown.
				time.Sleep(50 * time.Millisecond)
				// Explicitly test this on top of the leakdetection, as the latter uses Eventually
				// so might succeed even when the controller does not wait for all reconciliations
				// to finish.
				Expect(controllerFinished).NotTo(BeClosed())
				return reconcile.Result{}, nil
			})

			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{Reconciler: rec})
			Expect(c.Watch(watch, &handler.EnqueueRequestForObject{})).To(Succeed())
			Expect(err).NotTo(HaveOccurred())

			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).To(Succeed())
				close(controllerFinished)
			}()

			<-reconcileStarted
			cancel()
			<-controllerFinished

			// force-close keep-alive connections.  These'll time anyway (after
			// like 30s or so) but force it to speed up the tests.
			clientTransport.CloseIdleConnections()
			Eventually(func() error { return goleak.Find(currentGRs) }).Should(Succeed())
		})

		It("should not create goroutines if never started", func() {
			currentGRs := goleak.IgnoreCurrent()

			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			_, err = controller.New("new-controller", m, controller.Options{Reconciler: rec})
			Expect(err).NotTo(HaveOccurred())

			// force-close keep-alive connections.  These'll time anyway (after
			// like 30s or so) but force it to speed up the tests.
			clientTransport.CloseIdleConnections()
			Eventually(func() error { return goleak.Find(currentGRs) }).Should(Succeed())
		})

		It("should default RecoverPanic from the manager", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{RecoverPanic: pointer.Bool(true)}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.RecoverPanic).NotTo(BeNil())
			Expect(*ctrl.RecoverPanic).To(BeTrue())
		})

		It("should not override RecoverPanic on the controller", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{RecoverPanic: pointer.Bool(true)}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				RecoverPanic: pointer.Bool(false),
				Reconciler:   reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.RecoverPanic).NotTo(BeNil())
			Expect(*ctrl.RecoverPanic).To(BeFalse())
		})

		It("should default NeedLeaderElection from the manager", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{NeedLeaderElection: pointer.Bool(true)}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.NeedLeaderElection()).To(BeTrue())
		})

		It("should not override NeedLeaderElection on the controller", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{NeedLeaderElection: pointer.Bool(true)}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				NeedLeaderElection: pointer.Bool(false),
				Reconciler:         reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.NeedLeaderElection()).To(BeFalse())
		})

		It("Should default MaxConcurrentReconciles from the manager if set", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{MaxConcurrentReconciles: 5}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.MaxConcurrentReconciles).To(BeEquivalentTo(5))
		})

		It("Should default MaxConcurrentReconciles to 1 if unset", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.MaxConcurrentReconciles).To(BeEquivalentTo(1))
		})

		It("Should leave MaxConcurrentReconciles if set", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler:              reconcile.Func(nil),
				MaxConcurrentReconciles: 5,
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.MaxConcurrentReconciles).To(BeEquivalentTo(5))
		})

		It("Should default CacheSyncTimeout from the manager if set", func() {
			m, err := manager.New(cfg, manager.Options{Controller: config.Controller{CacheSyncTimeout: 5}})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.CacheSyncTimeout).To(BeEquivalentTo(5))
		})

		It("Should default CacheSyncTimeout to 2 minutes if unset", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: reconcile.Func(nil),
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.CacheSyncTimeout).To(BeEquivalentTo(2 * time.Minute))
		})

		It("Should leave CacheSyncTimeout if set", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler:       reconcile.Func(nil),
				CacheSyncTimeout: 5,
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.CacheSyncTimeout).To(BeEquivalentTo(5))
		})

		It("should default NeedLeaderElection on the controller to true", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: rec,
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.NeedLeaderElection()).To(BeTrue())
		})

		It("should allow for setting leaderElected to false", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				NeedLeaderElection: pointer.Bool(false),
				Reconciler:         rec,
			})
			Expect(err).NotTo(HaveOccurred())

			ctrl, ok := c.(*internalcontroller.Controller)
			Expect(ok).To(BeTrue())

			Expect(ctrl.NeedLeaderElection()).To(BeFalse())
		})

		It("should implement manager.LeaderElectionRunnable", func() {
			m, err := manager.New(cfg, manager.Options{})
			Expect(err).NotTo(HaveOccurred())

			c, err := controller.New("new-controller", m, controller.Options{
				Reconciler: rec,
			})
			Expect(err).NotTo(HaveOccurred())

			_, ok := c.(manager.LeaderElectionRunnable)
			Expect(ok).To(BeTrue())
		})
	})
})
