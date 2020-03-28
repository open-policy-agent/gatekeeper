/*

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
package readiness_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/onsi/gomega"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/controller"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.Logger(true))
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		HealthProbeBindAddress: "127.0.0.1:29090",
		MetricsBindAddress:     "0",
		NewCache:               dynamiccache.New,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
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

func setupOpa(t *testing.T) *opa.Client {
	// initialize OPA
	driver := local.New(local.Tracing(false))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		t.Fatalf("setting up OPA backend: %v", err)
	}
	client, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		t.Fatalf("setting up OPA client: %v", err)
	}
	return client
}

func setupController(mgr manager.Manager, wm *watch.Manager, opa *opa.Client) error {
	tracker, err := readiness.SetupTracker(mgr, wm)
	if err != nil {
		return fmt.Errorf("setting up tracker: %w", err)
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	sw := watch.NewSwitch()

	// Setup all Controllers
	opts := controller.Dependencies{
		Opa:              opa,
		WatchManger:      wm,
		ControllerSwitch: sw,
		Tracker:          tracker,
	}
	if err := controller.AddToManager(mgr, opts); err != nil {
		return fmt.Errorf("registering controllers: %w", err)
	}
	return nil
}

func Test_Tracker(t *testing.T) {
	g := gomega.NewWithT(t)

	// Apply fixtures *before* the controllers are setup.
	err := applyFixtures("testdata")
	g.Expect(err).NotTo(gomega.HaveOccurred(), "applying fixtures")

	// Wire up the rest.
	mgr, wm := setupManager(t)
	opaClient := setupOpa(t)
	if err := setupController(mgr, wm, opaClient); err != nil {
		t.Fatalf("setupControllers: %v", err)
	}

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:29090/readyz", http.NoBody)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "constructing http request")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()

		return resp.StatusCode >= 200 && resp.StatusCode < 400, nil
	}, 300*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// Verify cache (tracks testdata fixtures)
	ctx := context.Background()
	for _, ct := range testTemplates {
		_, err := opaClient.GetTemplate(ctx, ct)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "checking cache for template")
	}
	for _, c := range testConstraints {
		_, err := opaClient.GetConstraint(ctx, c)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "checking cache for constraint")
	}
	// TODO: Verify data if we add the corresponding API to opa.Client.
	//for _, d := range testData {
	//	_, err := opaClient.GetData(ctx, c)
	//	g.Expect(err).NotTo(gomega.HaveOccurred(), "checking cache for constraint")
	//}
}
