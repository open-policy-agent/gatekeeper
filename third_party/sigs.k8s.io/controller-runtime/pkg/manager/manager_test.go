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

package manager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/goleak"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	configv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	intrec "sigs.k8s.io/controller-runtime/pkg/internal/recorder"
	"sigs.k8s.io/controller-runtime/pkg/leaderelection"
	fakeleaderelection "sigs.k8s.io/controller-runtime/pkg/leaderelection/fake"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var _ = Describe("manger.Manager", func() {
	Describe("New", func() {
		It("should return an error if there is no Config", func() {
			m, err := New(nil, Options{})
			Expect(m).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("must specify Config"))

		})

		It("should return an error if it can't create a RestMapper", func() {
			expected := fmt.Errorf("expected error: RestMapper")
			m, err := New(cfg, Options{
				MapperProvider: func(c *rest.Config, httpClient *http.Client) (meta.RESTMapper, error) { return nil, expected },
			})
			Expect(m).To(BeNil())
			Expect(err).To(Equal(expected))

		})

		It("should return an error it can't create a client.Client", func() {
			m, err := New(cfg, Options{
				NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
					return nil, errors.New("expected error")
				},
			})
			Expect(m).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected error"))
		})

		It("should return an error it can't create a cache.Cache", func() {
			m, err := New(cfg, Options{
				NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
					return nil, fmt.Errorf("expected error")
				},
			})
			Expect(m).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected error"))
		})

		It("should create a client defined in by the new client function", func() {
			m, err := New(cfg, Options{
				NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
					return nil, nil
				},
			})
			Expect(m).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
			Expect(m.GetClient()).To(BeNil())
		})

		It("should return an error it can't create a recorder.Provider", func() {
			m, err := New(cfg, Options{
				newRecorderProvider: func(_ *rest.Config, _ *http.Client, _ *runtime.Scheme, _ logr.Logger, _ intrec.EventBroadcasterProducer) (*intrec.Provider, error) {
					return nil, fmt.Errorf("expected error")
				},
			})
			Expect(m).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected error"))
		})

		It("should be able to load Options from cfg.ControllerManagerConfiguration type", func() {
			duration := metav1.Duration{Duration: 48 * time.Hour}
			port := int(6090)
			leaderElect := false

			ccfg := &v1alpha1.ControllerManagerConfiguration{
				ControllerManagerConfigurationSpec: v1alpha1.ControllerManagerConfigurationSpec{
					SyncPeriod: &duration,
					LeaderElection: &configv1alpha1.LeaderElectionConfiguration{
						LeaderElect:       &leaderElect,
						ResourceLock:      "leases",
						ResourceNamespace: "default",
						ResourceName:      "ctrl-lease",
						LeaseDuration:     duration,
						RenewDeadline:     duration,
						RetryPeriod:       duration,
					},
					CacheNamespace: "default",
					Metrics: v1alpha1.ControllerMetrics{
						BindAddress: ":6000",
					},
					Health: v1alpha1.ControllerHealth{
						HealthProbeBindAddress: "6060",
						ReadinessEndpointName:  "/readyz",
						LivenessEndpointName:   "/livez",
					},
					Webhook: v1alpha1.ControllerWebhook{
						Port:    &port,
						Host:    "localhost",
						CertDir: "/certs",
					},
				},
			}

			m, err := Options{}.AndFrom(&fakeDeferredLoader{ccfg})
			Expect(err).To(BeNil())

			Expect(*m.SyncPeriod).To(Equal(duration.Duration))
			Expect(m.LeaderElection).To(Equal(leaderElect))
			Expect(m.LeaderElectionResourceLock).To(Equal("leases"))
			Expect(m.LeaderElectionNamespace).To(Equal("default"))
			Expect(m.LeaderElectionID).To(Equal("ctrl-lease"))
			Expect(m.LeaseDuration.String()).To(Equal(duration.Duration.String()))
			Expect(m.RenewDeadline.String()).To(Equal(duration.Duration.String()))
			Expect(m.RetryPeriod.String()).To(Equal(duration.Duration.String()))
			Expect(m.Namespace).To(Equal("default"))
			Expect(m.MetricsBindAddress).To(Equal(":6000"))
			Expect(m.HealthProbeBindAddress).To(Equal("6060"))
			Expect(m.ReadinessEndpointName).To(Equal("/readyz"))
			Expect(m.LivenessEndpointName).To(Equal("/livez"))
			Expect(m.Port).To(Equal(port))
			Expect(m.Host).To(Equal("localhost"))
			Expect(m.CertDir).To(Equal("/certs"))
		})

		It("should be able to keep Options when cfg.ControllerManagerConfiguration set", func() {
			optDuration := time.Duration(2)
			duration := metav1.Duration{Duration: 48 * time.Hour}
			port := int(6090)
			leaderElect := false

			ccfg := &v1alpha1.ControllerManagerConfiguration{
				ControllerManagerConfigurationSpec: v1alpha1.ControllerManagerConfigurationSpec{
					SyncPeriod: &duration,
					LeaderElection: &configv1alpha1.LeaderElectionConfiguration{
						LeaderElect:       &leaderElect,
						ResourceLock:      "leases",
						ResourceNamespace: "default",
						ResourceName:      "ctrl-lease",
						LeaseDuration:     duration,
						RenewDeadline:     duration,
						RetryPeriod:       duration,
					},
					CacheNamespace: "default",
					Metrics: v1alpha1.ControllerMetrics{
						BindAddress: ":6000",
					},
					Health: v1alpha1.ControllerHealth{
						HealthProbeBindAddress: "6060",
						ReadinessEndpointName:  "/readyz",
						LivenessEndpointName:   "/livez",
					},
					Webhook: v1alpha1.ControllerWebhook{
						Port:    &port,
						Host:    "localhost",
						CertDir: "/certs",
					},
				},
			}

			optionsTlSOptsFuncs := []func(*tls.Config){
				func(config *tls.Config) {},
			}
			m, err := Options{
				SyncPeriod:                 &optDuration,
				LeaderElection:             true,
				LeaderElectionResourceLock: "configmaps",
				LeaderElectionNamespace:    "ctrl",
				LeaderElectionID:           "ctrl-configmap",
				LeaseDuration:              &optDuration,
				RenewDeadline:              &optDuration,
				RetryPeriod:                &optDuration,
				Namespace:                  "ctrl",
				MetricsBindAddress:         ":7000",
				HealthProbeBindAddress:     "5000",
				ReadinessEndpointName:      "/readiness",
				LivenessEndpointName:       "/liveness",
				WebhookServer: webhook.NewServer(webhook.Options{
					Port:    8080,
					Host:    "example.com",
					CertDir: "/pki",
					TLSOpts: optionsTlSOptsFuncs,
				}),
			}.AndFrom(&fakeDeferredLoader{ccfg})
			Expect(err).To(BeNil())

			Expect(m.SyncPeriod.String()).To(Equal(optDuration.String()))
			Expect(m.LeaderElection).To(Equal(true))
			Expect(m.LeaderElectionResourceLock).To(Equal("configmaps"))
			Expect(m.LeaderElectionNamespace).To(Equal("ctrl"))
			Expect(m.LeaderElectionID).To(Equal("ctrl-configmap"))
			Expect(m.LeaseDuration.String()).To(Equal(optDuration.String()))
			Expect(m.RenewDeadline.String()).To(Equal(optDuration.String()))
			Expect(m.RetryPeriod.String()).To(Equal(optDuration.String()))
			Expect(m.Namespace).To(Equal("ctrl"))
			Expect(m.MetricsBindAddress).To(Equal(":7000"))
			Expect(m.HealthProbeBindAddress).To(Equal("5000"))
			Expect(m.ReadinessEndpointName).To(Equal("/readiness"))
			Expect(m.LivenessEndpointName).To(Equal("/liveness"))
			Expect(m.WebhookServer.(*webhook.DefaultServer).Options.Port).To(Equal(8080))
			Expect(m.WebhookServer.(*webhook.DefaultServer).Options.Host).To(Equal("example.com"))
			Expect(m.WebhookServer.(*webhook.DefaultServer).Options.CertDir).To(Equal("/pki"))
			Expect(m.WebhookServer.(*webhook.DefaultServer).Options.TLSOpts).To(Equal(optionsTlSOptsFuncs))
		})

		It("should lazily initialize a webhook server if needed", func() {
			By("creating a manager with options")
			m, err := New(cfg, Options{WebhookServer: webhook.NewServer(webhook.Options{Port: 9440, Host: "foo.com"})})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())

			By("checking options are passed to the webhook server")
			svr := m.GetWebhookServer()
			Expect(svr).NotTo(BeNil())
			Expect(svr.(*webhook.DefaultServer).Options.Port).To(Equal(9440))
			Expect(svr.(*webhook.DefaultServer).Options.Host).To(Equal("foo.com"))
		})

		It("should lazily initialize a webhook server if needed (deprecated)", func() {
			By("creating a manager with options")
			m, err := New(cfg, Options{Port: 9440, Host: "foo.com"})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())

			By("checking options are passed to the webhook server")
			svr := m.GetWebhookServer()
			Expect(svr).NotTo(BeNil())
			Expect(svr.(*webhook.DefaultServer).Options.Port).To(Equal(9440))
			Expect(svr.(*webhook.DefaultServer).Options.Host).To(Equal("foo.com"))
		})

		It("should not initialize a webhook server if Options.WebhookServer is set", func() {
			By("creating a manager with options")
			srv := webhook.NewServer(webhook.Options{Port: 9440})
			m, err := New(cfg, Options{Port: 9441, WebhookServer: srv})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())

			By("checking the server contains the Port set on the webhook server and not passed to Options")
			svr := m.GetWebhookServer()
			Expect(svr).NotTo(BeNil())
			Expect(svr).To(Equal(srv))
			Expect(svr.(*webhook.DefaultServer).Options.Port).To(Equal(9440))
		})

		It("should allow passing a custom webhook.Server implementation", func() {
			type customWebhook struct {
				webhook.Server
			}
			m, err := New(cfg, Options{WebhookServer: customWebhook{}})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())

			svr := m.GetWebhookServer()
			Expect(svr).NotTo(BeNil())

			_, isCustomWebhook := svr.(customWebhook)
			Expect(isCustomWebhook).To(BeTrue())
		})

		Context("with leader election enabled", func() {
			It("should only cancel the leader election after all runnables are done", func() {
				m, err := New(cfg, Options{
					LeaderElection:          true,
					LeaderElectionNamespace: "default",
					LeaderElectionID:        "test-leader-election-id-2",
					HealthProbeBindAddress:  "0",
					MetricsBindAddress:      "0",
					PprofBindAddress:        "0",
				})
				Expect(err).To(BeNil())

				runnableDone := make(chan struct{})
				slowRunnable := RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(100 * time.Millisecond)
					close(runnableDone)
					return nil
				})
				Expect(m.Add(slowRunnable)).To(BeNil())

				cm := m.(*controllerManager)
				cm.gracefulShutdownTimeout = time.Second
				leaderElectionDone := make(chan struct{})
				cm.onStoppedLeading = func() {
					close(leaderElectionDone)
				}

				ctx, cancel := context.WithCancel(context.Background())
				mgrDone := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).To(BeNil())
					close(mgrDone)
				}()
				<-cm.Elected()
				cancel()
				select {
				case <-leaderElectionDone:
					Expect(errors.New("leader election was cancelled before runnables were done")).ToNot(HaveOccurred())
				case <-runnableDone:
					// Success
				}
				// Don't leak routines
				<-mgrDone

			})
			It("should disable gracefulShutdown when stopping to lead", func() {
				m, err := New(cfg, Options{
					LeaderElection:          true,
					LeaderElectionNamespace: "default",
					LeaderElectionID:        "test-leader-election-id-3",
					HealthProbeBindAddress:  "0",
					MetricsBindAddress:      "0",
					PprofBindAddress:        "0",
				})
				Expect(err).To(BeNil())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				mgrDone := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					err := m.Start(ctx)
					Expect(err).ToNot(BeNil())
					Expect(err.Error()).To(ContainSubstring("leader election lost"))
					close(mgrDone)
				}()
				cm := m.(*controllerManager)
				<-cm.elected

				cm.leaderElectionCancel()
				<-mgrDone

				Expect(cm.gracefulShutdownTimeout.Nanoseconds()).To(Equal(int64(0)))
			})
			It("should default ID to controller-runtime if ID is not set", func() {
				var rl resourcelock.Interface
				m1, err := New(cfg, Options{
					LeaderElection:          true,
					LeaderElectionNamespace: "default",
					LeaderElectionID:        "test-leader-election-id",
					newResourceLock: func(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error) {
						var err error
						rl, err = leaderelection.NewResourceLock(config, recorderProvider, options)
						return rl, err
					},
					HealthProbeBindAddress: "0",
					MetricsBindAddress:     "0",
					PprofBindAddress:       "0",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(m1).ToNot(BeNil())
				Expect(rl.Describe()).To(Equal("default/test-leader-election-id"))

				m1cm, ok := m1.(*controllerManager)
				Expect(ok).To(BeTrue())
				m1cm.onStoppedLeading = func() {}

				m2, err := New(cfg, Options{
					LeaderElection:          true,
					LeaderElectionNamespace: "default",
					LeaderElectionID:        "test-leader-election-id",
					newResourceLock: func(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error) {
						var err error
						rl, err = leaderelection.NewResourceLock(config, recorderProvider, options)
						return rl, err
					},
					HealthProbeBindAddress: "0",
					MetricsBindAddress:     "0",
					PprofBindAddress:       "0",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(m2).ToNot(BeNil())
				Expect(rl.Describe()).To(Equal("default/test-leader-election-id"))

				m2cm, ok := m2.(*controllerManager)
				Expect(ok).To(BeTrue())
				m2cm.onStoppedLeading = func() {}

				c1 := make(chan struct{})
				Expect(m1.Add(RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					close(c1)
					return nil
				}))).To(Succeed())

				ctx1, cancel1 := context.WithCancel(context.Background())
				defer cancel1()
				go func() {
					defer GinkgoRecover()
					Expect(m1.Elected()).ShouldNot(BeClosed())
					Expect(m1.Start(ctx1)).NotTo(HaveOccurred())
				}()
				<-m1.Elected()
				<-c1

				c2 := make(chan struct{})
				Expect(m2.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					close(c2)
					return nil
				}))).To(Succeed())

				ctx2, cancel := context.WithCancel(context.Background())
				m2done := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					Expect(m2.Start(ctx2)).NotTo(HaveOccurred())
					close(m2done)
				}()
				Consistently(m2.Elected()).ShouldNot(Receive())

				Consistently(c2).ShouldNot(Receive())
				cancel()
				<-m2done
			})

			It("should return an error if it can't create a ResourceLock", func() {
				m, err := New(cfg, Options{
					newResourceLock: func(_ *rest.Config, _ recorder.Provider, _ leaderelection.Options) (resourcelock.Interface, error) {
						return nil, fmt.Errorf("expected error")
					},
				})
				Expect(m).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("expected error")))
			})

			It("should return an error if namespace not set and not running in cluster", func() {
				m, err := New(cfg, Options{LeaderElection: true, LeaderElectionID: "controller-runtime"})
				Expect(m).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unable to find leader election namespace: not running in-cluster, please specify LeaderElectionNamespace"))
			})

			// We must keep this default until we are sure all controller-runtime users have upgraded from the original default
			// ConfigMap lock to a controller-runtime version that has this new default. Many users of controller-runtime skip
			// versions, so we should be extremely conservative here.
			It("should default to LeasesResourceLock", func() {
				m, err := New(cfg, Options{LeaderElection: true, LeaderElectionID: "controller-runtime", LeaderElectionNamespace: "my-ns"})
				Expect(m).ToNot(BeNil())
				Expect(err).ToNot(HaveOccurred())
				cm, ok := m.(*controllerManager)
				Expect(ok).To(BeTrue())
				_, isLeaseLock := cm.resourceLock.(*resourcelock.LeaseLock)
				Expect(isLeaseLock).To(BeTrue())

			})
			It("should use the specified ResourceLock", func() {
				m, err := New(cfg, Options{
					LeaderElection:             true,
					LeaderElectionResourceLock: resourcelock.ConfigMapsLeasesResourceLock,
					LeaderElectionID:           "controller-runtime",
					LeaderElectionNamespace:    "my-ns",
				})
				Expect(m).ToNot(BeNil())
				Expect(err).ToNot(HaveOccurred())
				cm, ok := m.(*controllerManager)
				Expect(ok).To(BeTrue())
				multilock, isMultiLock := cm.resourceLock.(*resourcelock.MultiLock)
				Expect(isMultiLock).To(BeTrue())
				primaryLockType := reflect.TypeOf(multilock.Primary)
				Expect(primaryLockType.Kind()).To(Equal(reflect.Ptr))
				Expect(primaryLockType.Elem().PkgPath()).To(Equal("k8s.io/client-go/tools/leaderelection/resourcelock"))
				Expect(primaryLockType.Elem().Name()).To(Equal("configMapLock"))
				_, secondaryIsLeaseLock := multilock.Secondary.(*resourcelock.LeaseLock)
				Expect(secondaryIsLeaseLock).To(BeTrue())
			})
			It("should release lease if ElectionReleaseOnCancel is true", func() {
				var rl resourcelock.Interface
				m, err := New(cfg, Options{
					LeaderElection:                true,
					LeaderElectionResourceLock:    resourcelock.LeasesResourceLock,
					LeaderElectionID:              "controller-runtime",
					LeaderElectionNamespace:       "my-ns",
					LeaderElectionReleaseOnCancel: true,
					newResourceLock: func(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error) {
						var err error
						rl, err = fakeleaderelection.NewResourceLock(config, recorderProvider, options)
						return rl, err
					},
				})
				Expect(err).To(BeNil())

				ctx, cancel := context.WithCancel(context.Background())
				doneCh := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					defer close(doneCh)
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.(*controllerManager).elected
				cancel()
				<-doneCh

				ctx, cancel = context.WithCancel(context.Background())
				defer cancel()
				record, _, err := rl.Get(ctx)
				Expect(err).To(BeNil())
				Expect(record.HolderIdentity).To(BeEmpty())
			})
			When("using a custom LeaderElectionResourceLockInterface", func() {
				It("should use the custom LeaderElectionResourceLockInterface", func() {
					rl, err := fakeleaderelection.NewResourceLock(nil, nil, leaderelection.Options{})
					Expect(err).NotTo(HaveOccurred())

					m, err := New(cfg, Options{
						LeaderElection:                      true,
						LeaderElectionResourceLockInterface: rl,
						newResourceLock: func(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error) {
							return nil, fmt.Errorf("this should not be called")
						},
					})
					Expect(m).ToNot(BeNil())
					Expect(err).ToNot(HaveOccurred())
					cm, ok := m.(*controllerManager)
					Expect(ok).To(BeTrue())
					Expect(cm.resourceLock).To(Equal(rl))
				})
			})
		})

		It("should create a listener for the metrics if a valid address is provided", func() {
			var listener net.Listener
			m, err := New(cfg, Options{
				MetricsBindAddress: ":0",
				newMetricsListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = metrics.NewListener(addr)
					return listener, err
				},
			})
			Expect(m).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
			Expect(listener).ToNot(BeNil())
			Expect(listener.Close()).ToNot(HaveOccurred())
		})

		It("should return an error if the metrics bind address is already in use", func() {
			ln, err := metrics.NewListener(":0")
			Expect(err).ShouldNot(HaveOccurred())

			var listener net.Listener
			m, err := New(cfg, Options{
				MetricsBindAddress: ln.Addr().String(),
				newMetricsListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = metrics.NewListener(addr)
					return listener, err
				},
			})
			Expect(m).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(listener).To(BeNil())

			Expect(ln.Close()).ToNot(HaveOccurred())
		})

		It("should create a listener for the health probes if a valid address is provided", func() {
			var listener net.Listener
			m, err := New(cfg, Options{
				HealthProbeBindAddress: ":0",
				newHealthProbeListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = defaultHealthProbeListener(addr)
					return listener, err
				},
			})
			Expect(m).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
			Expect(listener).ToNot(BeNil())
			Expect(listener.Close()).ToNot(HaveOccurred())
		})

		It("should return an error if the health probes bind address is already in use", func() {
			ln, err := defaultHealthProbeListener(":0")
			Expect(err).ShouldNot(HaveOccurred())

			var listener net.Listener
			m, err := New(cfg, Options{
				HealthProbeBindAddress: ln.Addr().String(),
				newHealthProbeListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = defaultHealthProbeListener(addr)
					return listener, err
				},
			})
			Expect(m).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(listener).To(BeNil())

			Expect(ln.Close()).ToNot(HaveOccurred())
		})
	})

	Describe("Start", func() {
		var startSuite = func(options Options, callbacks ...func(Manager)) {
			It("should Start each Component", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				var wgRunnableStarted sync.WaitGroup
				wgRunnableStarted.Add(2)
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					wgRunnableStarted.Done()
					return nil
				}))).To(Succeed())

				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					wgRunnableStarted.Done()
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Elected()).ShouldNot(BeClosed())
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()

				<-m.Elected()
				wgRunnableStarted.Wait()
			})

			It("should not manipulate the provided config", func() {
				// strip WrapTransport, cause func values are PartialEq, not Eq --
				// specifically, for reflect.DeepEqual, for all functions F,
				// F != nil implies F != F, which means no full equivalence relation.
				cfg := rest.CopyConfig(cfg)
				cfg.WrapTransport = nil
				originalCfg := rest.CopyConfig(cfg)
				// The options object is shared by multiple tests, copy it
				// into our scope so we manipulate it for this testcase only
				options := options
				options.newResourceLock = nil
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				Expect(m.GetConfig()).To(Equal(originalCfg))
			})

			It("should stop when context is cancelled", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			})

			It("should return an error if it can't start the cache", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				mgr, ok := m.(*controllerManager)
				Expect(ok).To(BeTrue())
				Expect(mgr.Add(
					&cacheProvider{cache: &informertest.FakeInformers{Error: fmt.Errorf("expected error")}},
				)).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				Expect(m.Start(ctx)).To(MatchError(ContainSubstring("expected error")))
			})

			It("should start the cache before starting anything else", func() {
				fakeCache := &startSignalingInformer{Cache: &informertest.FakeInformers{}}
				options.NewCache = func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
					return fakeCache, nil
				}
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}

				runnableWasStarted := make(chan struct{})
				runnable := RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					if !fakeCache.wasSynced {
						return errors.New("runnable got started before cache was synced")
					}
					close(runnableWasStarted)
					return nil
				})
				Expect(m.Add(runnable)).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).ToNot(HaveOccurred())
				}()

				<-runnableWasStarted
			})

			It("should start additional clusters before anything else", func() {
				fakeCache := &startSignalingInformer{Cache: &informertest.FakeInformers{}}
				options.NewCache = func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
					return fakeCache, nil
				}
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}

				additionalClusterCache := &startSignalingInformer{Cache: &informertest.FakeInformers{}}
				additionalCluster, err := cluster.New(cfg, func(o *cluster.Options) {
					o.NewCache = func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
						return additionalClusterCache, nil
					}
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(m.Add(additionalCluster)).NotTo(HaveOccurred())

				runnableWasStarted := make(chan struct{})
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					if !fakeCache.wasSynced {
						return errors.New("WaitForCacheSyncCalled wasn't called before Runnable got started")
					}
					if !additionalClusterCache.wasSynced {
						return errors.New("the additional clusters WaitForCacheSync wasn't called before Runnable got started")
					}
					close(runnableWasStarted)
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).ToNot(HaveOccurred())
				}()

				<-runnableWasStarted
			})

			It("should return an error if any Components fail to Start", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}

				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					<-ctx.Done()
					return nil
				}))).To(Succeed())

				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					return fmt.Errorf("expected error")
				}))).To(Succeed())

				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					return nil
				}))).To(Succeed())

				defer GinkgoRecover()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				err = m.Start(ctx)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(Equal("expected error"))
			})

			It("should start caches added after Manager has started", func() {
				fakeCache := &startSignalingInformer{Cache: &informertest.FakeInformers{}}
				options.NewCache = func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
					return fakeCache, nil
				}
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}

				runnableWasStarted := make(chan struct{})
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					if !fakeCache.wasSynced {
						return errors.New("WaitForCacheSyncCalled wasn't called before Runnable got started")
					}
					close(runnableWasStarted)
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).ToNot(HaveOccurred())
				}()

				<-runnableWasStarted

				additionalClusterCache := &startSignalingInformer{Cache: &informertest.FakeInformers{}}
				fakeCluster := &startClusterAfterManager{informer: additionalClusterCache}

				Expect(err).NotTo(HaveOccurred())
				Expect(m.Add(fakeCluster)).NotTo(HaveOccurred())

				Eventually(func() bool {
					fakeCluster.informer.mu.Lock()
					defer fakeCluster.informer.mu.Unlock()
					return fakeCluster.informer.wasStarted && fakeCluster.informer.wasSynced
				}).Should(BeTrue())
			})

			It("should wait for runnables to stop", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}

				var lock sync.Mutex
				var runnableDoneCount int64
				runnableDoneFunc := func() {
					lock.Lock()
					defer lock.Unlock()
					atomic.AddInt64(&runnableDoneCount, 1)
				}
				var wgRunnableRunning sync.WaitGroup
				wgRunnableRunning.Add(2)
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					wgRunnableRunning.Done()
					defer GinkgoRecover()
					defer runnableDoneFunc()
					<-ctx.Done()
					return nil
				}))).To(Succeed())

				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					wgRunnableRunning.Done()
					defer GinkgoRecover()
					defer runnableDoneFunc()
					<-ctx.Done()
					time.Sleep(300 * time.Millisecond) // slow closure simulation
					return nil
				}))).To(Succeed())

				defer GinkgoRecover()
				ctx, cancel := context.WithCancel(context.Background())

				var wgManagerRunning sync.WaitGroup
				wgManagerRunning.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wgManagerRunning.Done()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
					Eventually(func() int64 {
						return atomic.LoadInt64(&runnableDoneCount)
					}).Should(BeEquivalentTo(2))
				}()
				wgRunnableRunning.Wait()
				cancel()

				wgManagerRunning.Wait()
			})

			It("should return an error if any Components fail to Start and wait for runnables to stop", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				defer GinkgoRecover()
				var lock sync.Mutex
				runnableDoneCount := 0
				runnableDoneFunc := func() {
					lock.Lock()
					defer lock.Unlock()
					runnableDoneCount++
				}

				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					defer runnableDoneFunc()
					return fmt.Errorf("expected error")
				}))).To(Succeed())

				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					defer GinkgoRecover()
					defer runnableDoneFunc()
					<-ctx.Done()
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				Expect(m.Start(ctx)).To(HaveOccurred())
				Expect(runnableDoneCount).To(Equal(2))
			})

			It("should refuse to add runnable if stop procedure is already engaged", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				defer GinkgoRecover()

				var wgRunnableRunning sync.WaitGroup
				wgRunnableRunning.Add(1)
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					wgRunnableRunning.Done()
					defer GinkgoRecover()
					<-ctx.Done()
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				wgRunnableRunning.Wait()
				cancel()
				time.Sleep(100 * time.Millisecond) // give some time for the stop chan closure to be caught by the manager
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					return nil
				}))).NotTo(Succeed())
			})

			It("should return both runnables and stop errors when both error", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				m.(*controllerManager).gracefulShutdownTimeout = 1 * time.Nanosecond
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					return runnableError{}
				})))
				testDone := make(chan struct{})
				defer close(testDone)
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					timer := time.NewTimer(30 * time.Second)
					defer timer.Stop()
					select {
					case <-testDone:
						return nil
					case <-timer.C:
						return nil
					}
				})))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				err = m.Start(ctx)
				Expect(err).ToNot(BeNil())
				eMsg := "[not feeling like that, failed waiting for all runnables to end within grace period of 1ns: context deadline exceeded]"
				Expect(err.Error()).To(Equal(eMsg))
				Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
				Expect(errors.Is(err, runnableError{})).To(BeTrue())
			})

			It("should return only stop errors if runnables dont error", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				m.(*controllerManager).gracefulShutdownTimeout = 1 * time.Nanosecond
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				})))
				testDone := make(chan struct{})
				defer close(testDone)
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					timer := time.NewTimer(30 * time.Second)
					defer timer.Stop()
					select {
					case <-testDone:
						return nil
					case <-timer.C:
						return nil
					}
				}))).NotTo(HaveOccurred())
				ctx, cancel := context.WithCancel(context.Background())
				managerStopDone := make(chan struct{})
				go func() { err = m.Start(ctx); close(managerStopDone) }()
				// Use the 'elected' channel to find out if startup was done, otherwise we stop
				// before we started the Runnable and see flakes, mostly in low-CPU envs like CI
				<-m.(*controllerManager).elected
				cancel()
				<-managerStopDone
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(Equal("failed waiting for all runnables to end within grace period of 1ns: context deadline exceeded"))
				Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
				Expect(errors.Is(err, runnableError{})).ToNot(BeTrue())
			})

			It("should return only runnables error if stop doesn't error", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					return runnableError{}
				})))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				err = m.Start(ctx)
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(Equal("not feeling like that"))
				Expect(errors.Is(err, context.DeadlineExceeded)).ToNot(BeTrue())
				Expect(errors.Is(err, runnableError{})).To(BeTrue())
			})

			It("should not wait for runnables if gracefulShutdownTimeout is 0", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				m.(*controllerManager).gracefulShutdownTimeout = time.Duration(0)

				runnableStopped := make(chan struct{})
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(100 * time.Millisecond)
					close(runnableStopped)
					return nil
				}))).ToNot(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				managerStopDone := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
					close(managerStopDone)
				}()
				<-m.Elected()
				cancel()

				<-managerStopDone
				<-runnableStopped
			})

			It("should wait forever for runnables if gracefulShutdownTimeout is <0 (-1)", func() {
				m, err := New(cfg, options)
				Expect(err).NotTo(HaveOccurred())
				for _, cb := range callbacks {
					cb(m)
				}
				m.(*controllerManager).gracefulShutdownTimeout = time.Duration(-1)

				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(100 * time.Millisecond)
					return nil
				}))).ToNot(HaveOccurred())
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(200 * time.Millisecond)
					return nil
				}))).ToNot(HaveOccurred())
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(500 * time.Millisecond)
					return nil
				}))).ToNot(HaveOccurred())
				Expect(m.Add(RunnableFunc(func(ctx context.Context) error {
					<-ctx.Done()
					time.Sleep(1500 * time.Millisecond)
					return nil
				}))).ToNot(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				managerStopDone := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
					close(managerStopDone)
				}()
				<-m.Elected()
				cancel()

				beforeDone := time.Now()
				<-managerStopDone
				Expect(time.Since(beforeDone)).To(BeNumerically(">=", 1500*time.Millisecond))
			})

		}

		Context("with defaults", func() {
			startSuite(Options{})
		})

		Context("with leaderelection enabled", func() {
			startSuite(
				Options{
					LeaderElection:          true,
					LeaderElectionID:        "controller-runtime",
					LeaderElectionNamespace: "default",
					newResourceLock:         fakeleaderelection.NewResourceLock,
				},
				func(m Manager) {
					cm, ok := m.(*controllerManager)
					Expect(ok).To(BeTrue())
					cm.onStoppedLeading = func() {}
				},
			)
		})

		Context("should start serving metrics", func() {
			var listener net.Listener
			var opts Options

			BeforeEach(func() {
				listener = nil
				opts = Options{
					newMetricsListener: func(addr string) (net.Listener, error) {
						var err error
						listener, err = metrics.NewListener(addr)
						return listener, err
					},
				}
			})

			AfterEach(func() {
				if listener != nil {
					listener.Close()
				}
			})

			It("should stop serving metrics when stop is called", func() {
				opts.MetricsBindAddress = ":0"
				m, err := New(cfg, opts)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()

				// Check the metrics started
				endpoint := fmt.Sprintf("http://%s", listener.Addr().String())
				_, err = http.Get(endpoint)
				Expect(err).NotTo(HaveOccurred())

				// Shutdown the server
				cancel()

				// Expect the metrics server to shutdown
				Eventually(func() error {
					_, err = http.Get(endpoint)
					return err
				}).ShouldNot(Succeed())
			})

			It("should serve metrics endpoint", func() {
				opts.MetricsBindAddress = ":0"
				m, err := New(cfg, opts)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.Elected()

				metricsEndpoint := fmt.Sprintf("http://%s/metrics", listener.Addr().String())
				resp, err := http.Get(metricsEndpoint)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(200))
			})

			It("should not serve anything other than metrics endpoint by default", func() {
				opts.MetricsBindAddress = ":0"
				m, err := New(cfg, opts)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.Elected()

				endpoint := fmt.Sprintf("http://%s/should-not-exist", listener.Addr().String())
				resp, err := http.Get(endpoint)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(404))
			})

			It("should serve metrics in its registry", func() {
				one := prometheus.NewCounter(prometheus.CounterOpts{
					Name: "test_one",
					Help: "test metric for testing",
				})
				one.Inc()
				err := metrics.Registry.Register(one)
				Expect(err).NotTo(HaveOccurred())

				opts.MetricsBindAddress = ":0"
				m, err := New(cfg, opts)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.Elected()

				metricsEndpoint := fmt.Sprintf("http://%s/metrics", listener.Addr().String())
				resp, err := http.Get(metricsEndpoint)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(200))

				data, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(ContainSubstring("%s\n%s\n%s\n",
					`# HELP test_one test metric for testing`,
					`# TYPE test_one counter`,
					`test_one 1`,
				))

				// Unregister will return false if the metric was never registered
				ok := metrics.Registry.Unregister(one)
				Expect(ok).To(BeTrue())
			})

			It("should serve extra endpoints", func() {
				opts.MetricsBindAddress = ":0"
				m, err := New(cfg, opts)
				Expect(err).NotTo(HaveOccurred())

				err = m.AddMetricsExtraHandler("/debug", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					_, _ = w.Write([]byte("Some debug info"))
				}))
				Expect(err).NotTo(HaveOccurred())

				// Should error when we add another extra endpoint on the already registered path.
				err = m.AddMetricsExtraHandler("/debug", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					_, _ = w.Write([]byte("Another debug info"))
				}))
				Expect(err).To(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.Elected()

				endpoint := fmt.Sprintf("http://%s/debug", listener.Addr().String())
				resp, err := http.Get(endpoint)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(body)).To(Equal("Some debug info"))
			})
		})
	})

	Context("should start serving health probes", func() {
		var listener net.Listener
		var opts Options

		BeforeEach(func() {
			listener = nil
			opts = Options{
				newHealthProbeListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = defaultHealthProbeListener(addr)
					return listener, err
				},
			}
		})

		AfterEach(func() {
			if listener != nil {
				listener.Close()
			}
		})

		It("should stop serving health probes when stop is called", func() {
			opts.HealthProbeBindAddress = ":0"
			m, err := New(cfg, opts)
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			<-m.Elected()

			// Check the health probes started
			endpoint := fmt.Sprintf("http://%s", listener.Addr().String())
			_, err = http.Get(endpoint)
			Expect(err).NotTo(HaveOccurred())

			// Shutdown the server
			cancel()

			// Expect the health probes server to shutdown
			Eventually(func() error {
				_, err = http.Get(endpoint)
				return err
			}, 10*time.Second).ShouldNot(Succeed())
		})

		It("should serve readiness endpoint", func() {
			opts.HealthProbeBindAddress = ":0"
			m, err := New(cfg, opts)
			Expect(err).NotTo(HaveOccurred())

			res := fmt.Errorf("not ready yet")
			namedCheck := "check"
			err = m.AddReadyzCheck(namedCheck, func(_ *http.Request) error { return res })
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			<-m.Elected()

			readinessEndpoint := fmt.Sprint("http://", listener.Addr().String(), defaultReadinessEndpoint)

			// Controller is not ready
			resp, err := http.Get(readinessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))

			// Controller is ready
			res = nil
			resp, err = http.Get(readinessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Check readiness path without trailing slash without redirect
			readinessEndpoint = fmt.Sprint("http://", listener.Addr().String(), defaultReadinessEndpoint)
			res = nil
			httpClient := http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse // Do not follow redirect
				},
			}
			resp, err = httpClient.Get(readinessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Check readiness path for individual check
			readinessEndpoint = fmt.Sprint("http://", listener.Addr().String(), path.Join(defaultReadinessEndpoint, namedCheck))
			res = nil
			resp, err = http.Get(readinessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("should serve liveness endpoint", func() {
			opts.HealthProbeBindAddress = ":0"
			m, err := New(cfg, opts)
			Expect(err).NotTo(HaveOccurred())

			res := fmt.Errorf("not alive")
			namedCheck := "check"
			err = m.AddHealthzCheck(namedCheck, func(_ *http.Request) error { return res })
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			<-m.Elected()

			livenessEndpoint := fmt.Sprint("http://", listener.Addr().String(), defaultLivenessEndpoint)

			// Controller is not ready
			resp, err := http.Get(livenessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))

			// Controller is ready
			res = nil
			resp, err = http.Get(livenessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Check liveness path without trailing slash without redirect
			livenessEndpoint = fmt.Sprint("http://", listener.Addr().String(), defaultLivenessEndpoint)
			res = nil
			httpClient := http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse // Do not follow redirect
				},
			}
			resp, err = httpClient.Get(livenessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Check readiness path for individual check
			livenessEndpoint = fmt.Sprint("http://", listener.Addr().String(), path.Join(defaultLivenessEndpoint, namedCheck))
			res = nil
			resp, err = http.Get(livenessEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Context("should start serving pprof", func() {
		var listener net.Listener
		var opts Options

		BeforeEach(func() {
			listener = nil
			opts = Options{
				newPprofListener: func(addr string) (net.Listener, error) {
					var err error
					listener, err = defaultPprofListener(addr)
					return listener, err
				},
			}
		})

		AfterEach(func() {
			if listener != nil {
				listener.Close()
			}
		})

		It("should stop serving pprof when stop is called", func() {
			opts.PprofBindAddress = ":0"
			m, err := New(cfg, opts)
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			<-m.Elected()

			// Check the pprof started
			endpoint := fmt.Sprintf("http://%s", listener.Addr().String())
			_, err = http.Get(endpoint)
			Expect(err).NotTo(HaveOccurred())

			// Shutdown the server
			cancel()

			// Expect the pprof server to shutdown
			Eventually(func() error {
				_, err = http.Get(endpoint)
				return err
			}, 10*time.Second).ShouldNot(Succeed())
		})

		It("should serve pprof endpoints", func() {
			opts.PprofBindAddress = ":0"
			m, err := New(cfg, opts)
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			<-m.Elected()

			pprofIndexEndpoint := fmt.Sprintf("http://%s/debug/pprof/", listener.Addr().String())
			resp, err := http.Get(pprofIndexEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			pprofCmdlineEndpoint := fmt.Sprintf("http://%s/debug/pprof/cmdline", listener.Addr().String())
			resp, err = http.Get(pprofCmdlineEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			pprofProfileEndpoint := fmt.Sprintf("http://%s/debug/pprof/profile", listener.Addr().String())
			resp, err = http.Get(pprofProfileEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			pprofSymbolEndpoint := fmt.Sprintf("http://%s/debug/pprof/symbol", listener.Addr().String())
			resp, err = http.Get(pprofSymbolEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			pprofTraceEndpoint := fmt.Sprintf("http://%s/debug/pprof/trace", listener.Addr().String())
			resp, err = http.Get(pprofTraceEndpoint)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Add", func() {
		It("should immediately start the Component if the Manager has already Started another Component",
			func() {
				m, err := New(cfg, Options{})
				Expect(err).NotTo(HaveOccurred())
				mgr, ok := m.(*controllerManager)
				Expect(ok).To(BeTrue())

				// Add one component before starting
				c1 := make(chan struct{})
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					close(c1)
					return nil
				}))).To(Succeed())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					defer GinkgoRecover()
					Expect(m.Start(ctx)).NotTo(HaveOccurred())
				}()
				<-m.Elected()

				// Wait for the Manager to start
				Eventually(func() bool {
					return mgr.runnables.Caches.Started()
				}).Should(BeTrue())

				// Add another component after starting
				c2 := make(chan struct{})
				Expect(m.Add(RunnableFunc(func(context.Context) error {
					defer GinkgoRecover()
					close(c2)
					return nil
				}))).To(Succeed())
				<-c1
				<-c2
			})

		It("should immediately start the Component if the Manager has already Started", func() {
			m, err := New(cfg, Options{})
			Expect(err).NotTo(HaveOccurred())
			mgr, ok := m.(*controllerManager)
			Expect(ok).To(BeTrue())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()

			// Wait for the Manager to start
			Eventually(func() bool {
				return mgr.runnables.Caches.Started()
			}).Should(BeTrue())

			c1 := make(chan struct{})
			Expect(m.Add(RunnableFunc(func(context.Context) error {
				defer GinkgoRecover()
				close(c1)
				return nil
			}))).To(Succeed())
			<-c1
		})

		It("should fail if attempted to start a second time", func() {
			m, err := New(cfg, Options{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer GinkgoRecover()
				Expect(m.Start(ctx)).NotTo(HaveOccurred())
			}()
			// Wait for the Manager to start
			Eventually(func() bool {
				mgr, ok := m.(*controllerManager)
				Expect(ok).To(BeTrue())
				return mgr.runnables.Caches.Started()
			}).Should(BeTrue())

			err = m.Start(ctx)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(Equal("manager already started"))

		})
	})

	It("should not leak goroutines when stopped", func() {
		currentGRs := goleak.IgnoreCurrent()

		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(m.Start(ctx)).NotTo(HaveOccurred())

		// force-close keep-alive connections.  These'll time anyway (after
		// like 30s or so) but force it to speed up the tests.
		clientTransport.CloseIdleConnections()
		Eventually(func() error { return goleak.Find(currentGRs) }).Should(Succeed())
	})

	It("should not leak goroutines if the default event broadcaster is used & events are emitted", func() {
		currentGRs := goleak.IgnoreCurrent()

		m, err := New(cfg, Options{ /* implicit: default setting for EventBroadcaster */ })
		Expect(err).NotTo(HaveOccurred())

		By("adding a runnable that emits an event")
		ns := corev1.Namespace{}
		ns.Name = "default"

		recorder := m.GetEventRecorderFor("rock-and-roll")
		Expect(m.Add(RunnableFunc(func(_ context.Context) error {
			recorder.Event(&ns, "Warning", "BallroomBlitz", "yeah, yeah, yeah-yeah-yeah")
			return nil
		}))).To(Succeed())

		By("starting the manager & waiting till we've sent our event")
		ctx, cancel := context.WithCancel(context.Background())
		doneCh := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(doneCh)
			Expect(m.Start(ctx)).To(Succeed())
		}()
		<-m.Elected()

		Eventually(func() *corev1.Event {
			evts, err := clientset.CoreV1().Events("").Search(m.GetScheme(), &ns)
			Expect(err).NotTo(HaveOccurred())

			for i, evt := range evts.Items {
				if evt.Reason == "BallroomBlitz" {
					return &evts.Items[i]
				}
			}
			return nil
		}).ShouldNot(BeNil())

		By("making sure there's no extra go routines still running after we stop")
		cancel()
		<-doneCh

		// force-close keep-alive connections.  These'll time anyway (after
		// like 30s or so) but force it to speed up the tests.
		clientTransport.CloseIdleConnections()
		Eventually(func() error { return goleak.Find(currentGRs) }).Should(Succeed())
	})

	It("should provide a function to get the Config", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		mgr, ok := m.(*controllerManager)
		Expect(ok).To(BeTrue())
		Expect(m.GetConfig()).To(Equal(mgr.cluster.GetConfig()))
	})

	It("should provide a function to get the Client", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		mgr, ok := m.(*controllerManager)
		Expect(ok).To(BeTrue())
		Expect(m.GetClient()).To(Equal(mgr.cluster.GetClient()))
	})

	It("should provide a function to get the Scheme", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		mgr, ok := m.(*controllerManager)
		Expect(ok).To(BeTrue())
		Expect(m.GetScheme()).To(Equal(mgr.cluster.GetScheme()))
	})

	It("should provide a function to get the FieldIndexer", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		mgr, ok := m.(*controllerManager)
		Expect(ok).To(BeTrue())
		Expect(m.GetFieldIndexer()).To(Equal(mgr.cluster.GetFieldIndexer()))
	})

	It("should provide a function to get the EventRecorder", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		Expect(m.GetEventRecorderFor("test")).NotTo(BeNil())
	})
	It("should provide a function to get the APIReader", func() {
		m, err := New(cfg, Options{})
		Expect(err).NotTo(HaveOccurred())
		Expect(m.GetAPIReader()).NotTo(BeNil())
	})
})

type runnableError struct {
}

func (runnableError) Error() string {
	return "not feeling like that"
}

var _ Runnable = &cacheProvider{}

type cacheProvider struct {
	cache cache.Cache
}

func (c *cacheProvider) GetCache() cache.Cache {
	return c.cache
}

func (c *cacheProvider) Start(ctx context.Context) error {
	return c.cache.Start(ctx)
}

type startSignalingInformer struct {
	mu sync.Mutex

	// The manager calls Start and WaitForCacheSync in
	// parallel, so we have to protect wasStarted with a Mutex
	// and block in WaitForCacheSync until it is true.
	wasStarted bool
	// was synced will be true once Start was called and
	// WaitForCacheSync returned, just like a real cache.
	wasSynced bool
	cache.Cache
}

func (c *startSignalingInformer) Start(ctx context.Context) error {
	c.mu.Lock()
	c.wasStarted = true
	c.mu.Unlock()
	return c.Cache.Start(ctx)
}

func (c *startSignalingInformer) WaitForCacheSync(ctx context.Context) bool {
	defer func() {
		c.mu.Lock()
		c.wasSynced = true
		c.mu.Unlock()
	}()
	return c.Cache.WaitForCacheSync(ctx)
}

type startClusterAfterManager struct {
	informer *startSignalingInformer
}

func (c *startClusterAfterManager) Start(ctx context.Context) error {
	return c.informer.Start(ctx)
}

func (c *startClusterAfterManager) GetCache() cache.Cache {
	return c.informer
}

type fakeDeferredLoader struct {
	*v1alpha1.ControllerManagerConfiguration
}

func (f *fakeDeferredLoader) Complete() (v1alpha1.ControllerManagerConfigurationSpec, error) {
	return f.ControllerManagerConfiguration.ControllerManagerConfigurationSpec, nil
}

func (f *fakeDeferredLoader) InjectScheme(scheme *runtime.Scheme) error {
	return nil
}
