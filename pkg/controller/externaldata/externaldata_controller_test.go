package externaldata

import (
	"context"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/test/clients"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const timeout = time.Second * 20

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{
	Name: "my-provider",
}}

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
		TypeMeta: metav1.TypeMeta{
			APIVersion: "externaldata.gatekeeper.sh/v1alpha1",
			Kind:       "Provider",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-provider",
		},
		Spec: externaldatav1alpha1.ProviderSpec{
			URL:                   "http://my-provider:8080",
			Timeout:               10,
			InsecureTLSSkipVerify: true,
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// force external data to be enabled
	*externaldata.ExternalDataEnabled = true
	pc := frameworksexternaldata.NewCache()

	// initialize OPA
	args := []local.Arg{local.Tracing(false), local.AddExternalDataProviderCache(pc)}
	driver, err := local.New(args...)
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	opa, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, true)
	if err != nil {
		t.Fatal(err)
	}

	rec := newReconciler(mgr, opa, pc, tracker)

	recFn, requests := SetupTestReconcile(rec)
	err = add(mgr, recFn)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	testutils.StartManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	defer testMgrStopped()

	t.Run("Can add a Provider object", func(t *testing.T) {
		err := c.Create(ctx, instance)
		if err != nil {
			t.Fatal(err)
		}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

		entry, err := pc.Get("my-provider")
		if err != nil {
			t.Fatal(err)
		}

		want := externaldatav1alpha1.ProviderSpec{
			URL:                   "http://my-provider:8080",
			Timeout:               10,
			InsecureTLSSkipVerify: true,
		}
		if diff := cmp.Diff(want, entry.Spec); diff != "" {
			t.Fatal(diff)
		}
	})

	t.Run("Can update a Provider object", func(t *testing.T) {
		newInstance := instance.DeepCopy()
		newInstance.Spec.Timeout = 20

		err := c.Update(ctx, newInstance)
		if err != nil {
			t.Fatal(err)
		}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

		entry, err := pc.Get("my-provider")
		if err != nil {
			t.Fatal(err)
		}

		wantSpec := externaldatav1alpha1.ProviderSpec{
			URL:                   "http://my-provider:8080",
			Timeout:               20,
			InsecureTLSSkipVerify: true,
		}
		if diff := cmp.Diff(wantSpec, entry.Spec); diff != "" {
			t.Fatal(diff)
		}
	})

	t.Run("Can delete a Provider object", func(t *testing.T) {
		err := c.Delete(ctx, instance)
		if err != nil {
			t.Fatal(err)
		}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

		_, err = pc.Get("my-provider")
		// TODO(willbeason): Make an error in frameworks for this test to check against
		//  so we don't rely on exact string matching.
		wantErr := "key is not found in provider cache"
		if err.Error() != wantErr {
			t.Fatalf("got error %v, want %v", err.Error(), wantErr)
		}
	})

	testMgrStopped()
	cs.Stop()
}
