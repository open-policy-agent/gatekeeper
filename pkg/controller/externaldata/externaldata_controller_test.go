package externaldata

import (
	"context"
	gosync "sync"
	"testing"

	"github.com/onsi/gomega"
	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/test/clients"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) manager.Manager {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	return mgr
}

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	instance := &externaldatav1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: externaldatav1alpha1.ProviderSpec{
			ProxyURL:      "http://test",
			FailurePolicy: "Fail",
			Timeout:       10,
			MaxRetry:      10,
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// initialize OPA
	driver := local.New(local.Tracing(true))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA backend: %s", err)
	}
	opa, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, true)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	pc := frameworksexternaldata.NewCache()
	rec := newReconciler(mgr, opa, pc, tracker)

	recFn, _ := SetupTestReconcile(rec)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancelFunc := context.WithCancel(context.Background())
	mgrStopped := StartTestManager(ctx, mgr, g)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	t.Run("Can add a provider object", func(t *testing.T) {
		g.Expect(c.Create(ctx, instance)).NotTo(gomega.HaveOccurred())
	})

	t.Run("Can delete a provider object", func(t *testing.T) {
		g.Expect(c.Delete(ctx, instance)).NotTo(gomega.HaveOccurred())
	})

	testMgrStopped()
	cs.Stop()
}
