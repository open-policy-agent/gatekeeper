package testutils

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
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
		fmt.Println("QWE123 -- Cleaning up manager") // TODO rm
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

	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           dynamiccache.New,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
		Logger: NewLogger(t),
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
