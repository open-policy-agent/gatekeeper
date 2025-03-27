package testutils

import (
	"context"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// StartManager starts mgr. Registers a cleanup function to stop the manager at the completion of the test.
func StartManager(ctx context.Context, t *testing.T, mgr manager.Manager) {
	ctx, cancel := context.WithCancel(ctx)

	mgrStopped := &sync.WaitGroup{}
	mgrStopped.Add(1)

	var err error
	go func() {
		defer mgrStopped.Done()
		err = mgr.Start(ctx)
	}()

	t.Cleanup(func() {
		cancel()

		mgrStopped.Wait()
		if err != nil {
			t.Errorf("running Manager: %v", err)
		}
	})
}

// SetupManager sets up a controller-runtime manager with registered watch manager.
func SetupManager(t *testing.T, cfg *rest.Config) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	skipNameValidation := true
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Logger:         NewLogger(t),
		Controller:    config.Controller{SkipNameValidation: &skipNameValidation},
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		t.Fatalf("expected dynamic cache, got: %T", c)
	}
	wm, err := watch.New(dc)
	if err != nil {
		t.Fatalf("could not create watch manager: %s", err)
	}
	if err := mgr.Add(wm); err != nil {
		t.Fatalf("could not add watch manager to manager: %s", err)
	}
	return mgr, wm
}
