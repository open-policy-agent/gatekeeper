package externaldata

import (
	"context"
	"fmt"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega"
	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const timeout = time.Second * 20

// generateUniqueName creates a unique resource name for testing to avoid race conditions
func generateUniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{
	Name: "my-provider", // This will be dynamically updated in tests
}}

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) manager.Manager {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	metrics.Registry = prometheus.NewRegistry()
	skipNameValidation := true
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		MapperProvider: apiutil.NewDynamicRESTMapper,
		Controller:     config.Controller{SkipNameValidation: &skipNameValidation},
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	return mgr
}

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	
	// Generate unique names for this test run to avoid race conditions
	providerName := generateUniqueName("my-provider")
	
	instance := &externaldatav1beta1.Provider{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "externaldata.gatekeeper.sh/v1beta1",
			Kind:       "Provider",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: providerName,
		},
		Spec: externaldatav1beta1.ProviderSpec{
			URL:      fmt.Sprintf("https://%s:8080", providerName),
			Timeout:  10,
			CABundle: util.ValidCABundle,
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// force external data to be enabled
	*externaldata.ExternalDataEnabled = true
	pc := frameworksexternaldata.NewCache()

	args := []rego.Arg{rego.Tracing(false), rego.AddExternalDataProviderCache(pc)}
	driver, err := rego.New(args...)
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	cfClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("unable to set up constraint framework client: %s", err)
	}

	tracker, err := readiness.SetupTracker(mgr, false, true, false)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	rec := newReconciler(mgr, cfClient, pc, tracker, func(context.Context) (*corev1.Pod, error) { return pod, nil })

	recFn, requests := SetupTestReconcile(rec)
	err = add(mgr, recFn)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	testutils.StartManager(ctx, t, mgr)

	// Create the gatekeeper-system namespace that the controller expects
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-system",
		},
	}
	err = c.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}

	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
		})
	}

	// Create dynamic expected request for this test
	dynamicExpectedRequest := reconcile.Request{NamespacedName: types.NamespacedName{
		Name: providerName,
	}}

	defer testMgrStopped()

	// Add cleanup to ensure resources are deleted after tests complete
	defer func() {
		if err := c.Delete(ctx, instance); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("Warning: failed to cleanup provider %s: %v", providerName, err)
		}
	}()

	t.Run("Can add a Provider object", func(t *testing.T) {
		err := c.Create(ctx, instance)
		if err != nil {
			t.Fatal(err)
		}
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(dynamicExpectedRequest)))

		entry, err := pc.Get(providerName)
		if err != nil {
			t.Fatal(err)
		}

		want := externaldataUnversioned.ProviderSpec{
			URL:      fmt.Sprintf("https://%s:8080", providerName),
			Timeout:  10,
			CABundle: util.ValidCABundle,
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
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(dynamicExpectedRequest)))

		entry, err := pc.Get(providerName)
		if err != nil {
			t.Fatal(err)
		}

		wantSpec := externaldataUnversioned.ProviderSpec{
			URL:      fmt.Sprintf("https://%s:8080", providerName),
			Timeout:  20,
			CABundle: util.ValidCABundle,
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
		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(dynamicExpectedRequest)))

		_, err = pc.Get(providerName)
		// TODO(willbeason): Make an error in frameworks for this test to check against
		//  so we don't rely on exact string matching.
		if err == nil {
			t.Fatal("expected error when getting deleted provider from cache, but got nil")
		}
		wantErr := "key is not found in provider cache"
		if err.Error() != wantErr {
			t.Fatalf("got error %v, want %v", err.Error(), wantErr)
		}
	})

	testMgrStopped()
}

func TestReconcile_MetricsIntegration(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mgr := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// Generate unique name for this test run to avoid race conditions
	providerName := generateUniqueName("metrics-test-provider")

	*externaldata.ExternalDataEnabled = true
	pc := frameworksexternaldata.NewCache()

	args := []rego.Arg{rego.Tracing(false), rego.AddExternalDataProviderCache(pc)}
	driver, err := rego.New(args...)
	require.NoError(t, err)

	cfClient, err := constraintclient.NewClient(
		constraintclient.Targets(&target.K8sValidationTarget{}),
		constraintclient.Driver(driver),
		constraintclient.EnforcementPoints(util.AuditEnforcementPoint),
	)
	require.NoError(t, err)

	tracker, err := readiness.SetupTracker(mgr, false, true, false)
	require.NoError(t, err)

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	rec := newReconciler(mgr, cfClient, pc, tracker, func(context.Context) (*corev1.Pod, error) {
		return pod, nil
	})

	// Create gatekeeper-system namespace if it doesn't exist
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-system"},
	}
	err = c.Create(context.Background(), ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testutils.StartManager(ctx, t, mgr)

	provider := &externaldatav1beta1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: providerName},
		Spec: externaldatav1beta1.ProviderSpec{
			URL:     fmt.Sprintf("https://%s:8080", providerName),
			Timeout: 10,
		},
	}

	// Add cleanup to ensure resources are deleted after test completes
	defer func() {
		if err := c.Delete(ctx, provider); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("Warning: failed to cleanup provider %s: %v", providerName, err)
		}
	}()

	// Create provider
	err = c.Create(ctx, provider)
	require.NoError(t, err)

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{Name: provider.Name},
	}

	// Reconcile and verify metrics are updated
	result, err := rec.Reconcile(ctx, request)
	assert.NoError(t, err)
	g.Expect(result).To(gomega.Equal(reconcile.Result{}))

	// Verify the provider was added to metrics with active status
	// This would require access to the metrics reporter's internal state
	// or mocking the metrics system
	assert.NotNil(t, rec.metrics)
}

func TestReconcile_ExternalDataDisabled(t *testing.T) {
	// Save original state
	originalState := *externaldata.ExternalDataEnabled
	defer func() {
		*externaldata.ExternalDataEnabled = originalState
	}()

	// Disable external data
	*externaldata.ExternalDataEnabled = false

	mgr := setupManager(t)

	// Try to add controller - should return early without error
	err := add(mgr, nil)
	assert.NoError(t, err)
}

func TestErrorChanged(t *testing.T) {
	tests := []struct {
		name      string
		oldErrors []*statusv1beta1.ProviderError
		newErrors []*statusv1beta1.ProviderError
		expected  bool
	}{
		{
			name:      "no errors to no errors",
			oldErrors: nil,
			newErrors: nil,
			expected:  false,
		},
		{
			name:      "no errors to some errors",
			oldErrors: nil,
			newErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			expected: true,
		},
		{
			name: "same errors",
			oldErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			newErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			expected: false,
		},
		{
			name: "different error messages",
			oldErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			newErrors: []*statusv1beta1.ProviderError{{
				Message: "error2",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			expected: true,
		},
		{
			name: "different error types",
			oldErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.UpsertCacheError,
			}},
			newErrors: []*statusv1beta1.ProviderError{{
				Message: "error1",
				Type:    statusv1beta1.ConversionError,
			}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errorChanged(tt.oldErrors, tt.newErrors)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetStatus(t *testing.T) {
	status := &statusv1beta1.ProviderPodStatus{}

	// Test with no errors
	setStatus(status, nil)
	assert.Nil(t, status.Status.Errors)
	assert.NotNil(t, status.Status.LastCacheUpdateTime)

	// Test with errors
	providerErrors := []*statusv1beta1.ProviderError{{
		Message: "test error",
		Type:    statusv1beta1.UpsertCacheError,
	}}
	setStatus(status, providerErrors)
	assert.Equal(t, providerErrors, status.Status.Errors)
	assert.Equal(t, false, status.Status.Active)

	setStatus(status, nil)
	assert.Nil(t, status.Status.Errors)
	assert.Equal(t, true, status.Status.Active)
}
