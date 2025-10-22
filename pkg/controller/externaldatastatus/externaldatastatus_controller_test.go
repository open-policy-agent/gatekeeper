package externaldatastatus

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Test operations on Provider and ProviderPodStatus handled by controller and reflected on Provider status.
func TestReconcile_E2E(t *testing.T) {
	// Setup
	const timeout = time.Second * 20
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("test-pod"))
		return pod, nil
	}
	pod, _ := getPod(ctx)

	// Wrap the controller Reconciler so it writes each request to a map when it is finished reconciling
	originalReconciler := newReconciler(mgr)
	wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
	// Register the controller with the manager
	require.NoError(t, add(mgr, wrappedReconciler))
	// Start the manager and let it run in the background
	testutils.StartManager(ctx, t, mgr)

	// Create the gatekeeper-system namespace that the controller expects
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-system",
		},
	}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}

	t.Run("Reconcile called and updates Provider status", func(t *testing.T) {
		providerObj := externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-provider",
			},
			Spec: externaldatav1beta1.ProviderSpec{
				URL:      "http://test-provider:8080",
				Timeout:  10,
				CABundle: "",
			},
		}
		typeProviderNamespacedName := types.NamespacedName{
			Name: "test-provider",
		}

		// Test setup - Create the provider object
		g.Expect(k8sClient.Create(ctx, &providerObj)).Should(gomega.Succeed())

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			// Use the Provider object for Reconcile request because of the Provider mapper
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Provider object should now exist
		providerObj = externaldatav1beta1.Provider{}
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.BeNil())
		}).WithTimeout(timeout).Should(gomega.Succeed(), "Provider object should exist after creation")

		// Next create the ProviderPodStatus object which should trigger the reconcile request
		providerPodStatusObjName, _ := statusv1beta1.KeyForProvider(pod.Name, providerObj.Name)
		typeProviderPodStatusNamespacedName := types.NamespacedName{
			Name:      providerPodStatusObjName,
			Namespace: util.GetNamespace(),
		}
		providerPodStatusObj := statusv1beta1.ProviderPodStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerPodStatusObjName,
				Namespace: util.GetNamespace(),
				Labels: map[string]string{
					statusv1beta1.ProviderNameLabel: providerObj.Name,
					statusv1beta1.PodLabel:          pod.Name,
				},
			},
			Status: statusv1beta1.ProviderPodStatusStatus{
				Active:              true,
				Errors:              []*statusv1beta1.ProviderError{},
				ObservedGeneration:  providerObj.GetGeneration(),
				ProviderUID:         providerObj.GetUID(),
				ID:                  pod.Name,
				Operations:          []string{},
				LastTransitionTime:  util.Now(),
				LastCacheUpdateTime: util.Now(),
			},
		}

		// Now create the provider pod status object which should trigger the reconcile request
		g.Expect(k8sClient.Create(ctx, &providerPodStatusObj)).Should(gomega.Succeed(), "Creating the provider pod status object should succeed")

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			// Use the Provider object for Reconcile request because of the Provider mapper
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Assert ProviderPodStatus object creation
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderPodStatusNamespacedName, &providerPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(providerPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ProviderNameLabel, providerObj.Name), "Status should have the correct provider name label")
			g.Expect(providerPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.PodLabel, pod.Name), "Status should have the correct pod label")
			g.Expect(providerPodStatusObj.Status.Errors).Should(gomega.BeEmpty(), "Status should not have an error after creation")
			g.Expect(providerPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(providerObj.GetGeneration()), "Observed generation should match the provider object generation")
			g.Expect(providerPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(providerPodStatusObj.Status.ProviderUID).Should(gomega.Equal(providerObj.GetUID()), "ProviderPodStatus UID should match the provider object UID")
			g.Expect(providerPodStatusObj.Status.Active).Should(gomega.BeTrue(), "Provider should be active")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Assert Provider object and its status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed(), "Provider should exist after updating the provider object")
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider object status should have one entry")
			g.Expect(providerObj.Status.ByPod[0].Errors).Should(gomega.BeEmpty(), "Status should not have an error after updating the provider object")
			g.Expect(providerObj.Status.ByPod[0].ObservedGeneration).Should(gomega.Equal(providerObj.GetGeneration()), "Observed generation should get updated to match the latest provider object generation after update")
			g.Expect(providerObj.Status.ByPod[0].ID).Should(gomega.Equal(pod.Name), "ID should still match the pod name after update")
			g.Expect(providerObj.Status.ByPod[0].ProviderUID).Should(gomega.Equal(providerObj.GetUID()), "ProviderPodStatus UID should still match the provider object UID after update")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeTrue(), "Provider should still be active")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Update of the Provider object
		providerObj.Spec.URL = "http://updated-provider:8080"
		g.Expect(k8sClient.Update(ctx, &providerObj)).Should(gomega.Succeed(), "Updating the Provider object should succeed")

		// Await for the reconcile request to finish after the update
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// The ProviderPodStatus should reflect the new generation
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider object status should still have one entry after update")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup
		g.Expect(k8sClient.Delete(ctx, &providerPodStatusObj)).Should(gomega.Succeed())
		g.Expect(k8sClient.Delete(ctx, &providerObj)).Should(gomega.Succeed())
	})

	t.Run("Reconcile with multiple pod statuses", func(t *testing.T) {
		providerObj := externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: "multi-pod-provider",
			},
			Spec: externaldatav1beta1.ProviderSpec{
				URL:      "http://multi-pod-provider:8080",
				Timeout:  10,
				CABundle: "",
			},
		}
		typeProviderNamespacedName := types.NamespacedName{
			Name: "multi-pod-provider",
		}

		// Create the provider object
		g.Expect(k8sClient.Create(ctx, &providerObj)).Should(gomega.Succeed())

		// Create multiple ProviderPodStatus objects
		podNames := []string{"pod-1", "pod-2", "pod-3"}
		var providerPodStatusObjs []statusv1beta1.ProviderPodStatus

		for i, podName := range podNames {
			providerPodStatusObjName, _ := statusv1beta1.KeyForProvider(podName, providerObj.Name)
			providerPodStatusObj := statusv1beta1.ProviderPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerPodStatusObjName,
					Namespace: util.GetNamespace(),
					Labels: map[string]string{
						statusv1beta1.ProviderNameLabel: providerObj.Name,
						statusv1beta1.PodLabel:          podName,
					},
				},
				Status: statusv1beta1.ProviderPodStatusStatus{
					Active:              i%2 == 0, // Alternate active status
					Errors:              []*statusv1beta1.ProviderError{},
					ObservedGeneration:  providerObj.GetGeneration(),
					ProviderUID:         providerObj.GetUID(),
					ID:                  podName,
					Operations:          []string{},
					LastTransitionTime:  util.Now(),
					LastCacheUpdateTime: util.Now(),
				},
			}
			providerPodStatusObjs = append(providerPodStatusObjs, providerPodStatusObj)
			g.Expect(k8sClient.Create(ctx, &providerPodStatusObj)).Should(gomega.Succeed())
		}

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that all pod statuses are reflected in the provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(3), "Provider should have status for all three pods")

			// Verify statuses are sorted by ID
			for i := 0; i < len(providerObj.Status.ByPod)-1; i++ {
				g.Expect(providerObj.Status.ByPod[i].ID < providerObj.Status.ByPod[i+1].ID).Should(gomega.BeTrue(), "Pod statuses should be sorted by ID")
			}
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup
		for _, obj := range providerPodStatusObjs {
			g.Expect(k8sClient.Delete(ctx, &obj)).Should(gomega.Succeed())
		}
		g.Expect(k8sClient.Delete(ctx, &providerObj)).Should(gomega.Succeed())
	})

	t.Run("Reconcile with status errors", func(t *testing.T) {
		providerObj := externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: "error-provider",
			},
			Spec: externaldatav1beta1.ProviderSpec{
				URL:      "http://error-provider:8080",
				Timeout:  10,
				CABundle: "",
			},
		}
		typeProviderNamespacedName := types.NamespacedName{
			Name: "error-provider",
		}

		// Create the provider object
		g.Expect(k8sClient.Create(ctx, &providerObj)).Should(gomega.Succeed())

		// Create ProviderPodStatus with errors
		providerPodStatusObjName, _ := statusv1beta1.KeyForProvider(pod.Name, providerObj.Name)
		providerPodStatusObj := statusv1beta1.ProviderPodStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerPodStatusObjName,
				Namespace: util.GetNamespace(),
				Labels: map[string]string{
					statusv1beta1.ProviderNameLabel: providerObj.Name,
					statusv1beta1.PodLabel:          pod.Name,
				},
			},
			Status: statusv1beta1.ProviderPodStatusStatus{
				Active: false,
				Errors: []*statusv1beta1.ProviderError{
					{
						Type:           statusv1beta1.ConversionError,
						Message:        "Failed to convert to unversioned external data provider",
						Retryable:      true,
						ErrorTimestamp: util.Now(),
					},
					{
						Type:           statusv1beta1.UpsertCacheError,
						Message:        "Failed to update external data provider cache",
						Retryable:      false,
						ErrorTimestamp: util.Now(),
					},
				},
				ObservedGeneration:  providerObj.GetGeneration(),
				ProviderUID:         providerObj.GetUID(),
				ID:                  pod.Name,
				Operations:          []string{},
				LastTransitionTime:  util.Now(),
				LastCacheUpdateTime: util.Now(),
			},
		}

		g.Expect(k8sClient.Create(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that errors are properly reflected in provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider should have one pod status")
			g.Expect(len(providerObj.Status.ByPod[0].Errors)).Should(gomega.Equal(2), "Provider status should have two errors")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "Provider should not be active due to errors")

			// Check error details
			errors := providerObj.Status.ByPod[0].Errors
			g.Expect(string(errors[0].Type)).Should(gomega.Equal(string(statusv1beta1.ConversionError)), "First error should be connection error")
			g.Expect(errors[0].Message).Should(gomega.Equal("Failed to convert to unversioned external data provider"))
			g.Expect(errors[0].Retryable).Should(gomega.BeTrue())

			g.Expect(string(errors[1].Type)).Should(gomega.Equal(string(statusv1beta1.UpsertCacheError)), "Second error should be query error")
			g.Expect(errors[1].Message).Should(gomega.Equal("Failed to update external data provider cache"))
			g.Expect(errors[1].Retryable).Should(gomega.BeFalse())
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup
		g.Expect(k8sClient.Delete(ctx, &providerPodStatusObj)).Should(gomega.Succeed())
		g.Expect(k8sClient.Delete(ctx, &providerObj)).Should(gomega.Succeed())
	})

	t.Run("Reconcile ignores status with wrong ProviderUID", func(t *testing.T) {
		providerObj := externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: "uid-test-provider",
			},
			Spec: externaldatav1beta1.ProviderSpec{
				URL:      "http://uid-test-provider:8080",
				Timeout:  10,
				CABundle: "",
			},
		}
		typeProviderNamespacedName := types.NamespacedName{
			Name: "uid-test-provider",
		}

		// Create the provider object
		g.Expect(k8sClient.Create(ctx, &providerObj)).Should(gomega.Succeed())

		tempTime := metav1.Now()
		// Create ProviderPodStatus with wrong ProviderUID
		providerPodStatusObjName, _ := statusv1beta1.KeyForProvider(pod.Name, providerObj.Name)
		providerPodStatusObj := statusv1beta1.ProviderPodStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerPodStatusObjName,
				Namespace: util.GetNamespace(),
				Labels: map[string]string{
					statusv1beta1.ProviderNameLabel: providerObj.Name,
					statusv1beta1.PodLabel:          pod.Name,
				},
			},
			Status: statusv1beta1.ProviderPodStatusStatus{
				Active:              true,
				Errors:              []*statusv1beta1.ProviderError{},
				ObservedGeneration:  providerObj.GetGeneration(),
				ProviderUID:         "wrong-uid-123", // Wrong UID
				ID:                  pod.Name,
				Operations:          operations.AssignedStringList(),
				LastTransitionTime:  &tempTime,
				LastCacheUpdateTime: &tempTime,
			},
		}

		g.Expect(k8sClient.Create(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Provider status should be empty because ProviderUID doesn't match
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(0), "Provider should have no pod status due to UID mismatch")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup
		g.Expect(k8sClient.Delete(ctx, &providerPodStatusObj)).Should(gomega.Succeed())
		g.Expect(k8sClient.Delete(ctx, &providerObj)).Should(gomega.Succeed())
	})

	t.Run("Reconcile with non-existent Provider", func(t *testing.T) {
		reconciler := newReconciler(mgr)

		// Try to reconcile a non-existent provider
		nonExistentRequest := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "non-existent-provider",
				Namespace: util.GetNamespace(),
			},
		}

		result, err := reconciler.Reconcile(ctx, nonExistentRequest)
		g.Expect(err).Should(gomega.BeNil(), "Reconciling non-existent provider should not return an error")
		g.Expect(result).Should(gomega.Equal(reconcile.Result{}), "Result should be empty for non-existent provider")
	})

	t.Run("Reconcile with status errors and recover", func(t *testing.T) {
		providerObj := externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: "error-provider-then-recover",
			},
			Spec: externaldatav1beta1.ProviderSpec{
				URL:      "http://error-provider:8080",
				Timeout:  10,
				CABundle: "",
			},
		}
		typeProviderNamespacedName := types.NamespacedName{
			Name: "error-provider-then-recover",
		}

		// Create the provider object
		g.Expect(k8sClient.Create(ctx, &providerObj)).Should(gomega.Succeed())

		// Create ProviderPodStatus with errors
		providerPodStatusObjName, _ := statusv1beta1.KeyForProvider(pod.Name, providerObj.Name)
		providerPodStatusObj := statusv1beta1.ProviderPodStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerPodStatusObjName,
				Namespace: util.GetNamespace(),
				Labels: map[string]string{
					statusv1beta1.ProviderNameLabel: providerObj.Name,
					statusv1beta1.PodLabel:          pod.Name,
				},
			},
			Status: statusv1beta1.ProviderPodStatusStatus{
				Active: false,
				Errors: []*statusv1beta1.ProviderError{
					{
						Type:           statusv1beta1.ConversionError,
						Message:        "Failed to convert to unversioned external data provider",
						Retryable:      true,
						ErrorTimestamp: util.Now(),
					},
					{
						Type:           statusv1beta1.UpsertCacheError,
						Message:        "Failed to update external data provider cache",
						Retryable:      false,
						ErrorTimestamp: util.Now(),
					},
				},
				ObservedGeneration:  providerObj.GetGeneration(),
				ProviderUID:         providerObj.GetUID(),
				ID:                  pod.Name,
				Operations:          []string{},
				LastTransitionTime:  util.Now(),
				LastCacheUpdateTime: util.Now(),
			},
		}

		g.Expect(k8sClient.Create(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that errors are properly reflected in provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider should have one pod status")
			g.Expect(len(providerObj.Status.ByPod[0].Errors)).Should(gomega.Equal(2), "Provider status should have two errors")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "Provider should not be active due to errors")

			// Check error details
			errors := providerObj.Status.ByPod[0].Errors
			g.Expect(string(errors[0].Type)).Should(gomega.Equal(string(statusv1beta1.ConversionError)), "First error should be connection error")
			g.Expect(errors[0].Message).Should(gomega.Equal("Failed to convert to unversioned external data provider"))
			g.Expect(errors[0].Retryable).Should(gomega.BeTrue())

			g.Expect(string(errors[1].Type)).Should(gomega.Equal(string(statusv1beta1.UpsertCacheError)), "Second error should be query error")
			g.Expect(errors[1].Message).Should(gomega.Equal("Failed to update external data provider cache"))
			g.Expect(errors[1].Retryable).Should(gomega.BeFalse())
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// transition from two errors to one error
		providerPodStatusObj.Status = statusv1beta1.ProviderPodStatusStatus{
			Active: false,
			Errors: []*statusv1beta1.ProviderError{
				{
					Type:           statusv1beta1.ConversionError,
					Message:        "Failed to convert to unversioned external data provider",
					Retryable:      true,
					ErrorTimestamp: util.Now(),
				},
			},
			ObservedGeneration:  providerObj.GetGeneration(),
			ProviderUID:         providerObj.GetUID(),
			ID:                  pod.Name,
			Operations:          []string{},
			LastTransitionTime:  util.Now(),
			LastCacheUpdateTime: util.Now(),
		}

		g.Expect(k8sClient.Update(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that errors are properly reflected in provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider should have one pod status")
			g.Expect(len(providerObj.Status.ByPod[0].Errors)).Should(gomega.Equal(1), "Provider status should have one error")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "Provider should not be active due to error")

			// Check error details
			errors := providerObj.Status.ByPod[0].Errors
			g.Expect(string(errors[0].Type)).Should(gomega.Equal(string(statusv1beta1.ConversionError)), "First error should be connection error")
			g.Expect(errors[0].Message).Should(gomega.Equal("Failed to convert to unversioned external data provider"))
			g.Expect(errors[0].Retryable).Should(gomega.BeTrue())
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// transition between types of error
		providerPodStatusObj.Status = statusv1beta1.ProviderPodStatusStatus{
			Active: false,
			Errors: []*statusv1beta1.ProviderError{
				{
					Type:           statusv1beta1.UpsertCacheError,
					Message:        "Failed to update external data provider cache",
					Retryable:      false,
					ErrorTimestamp: util.Now(),
				},
			},
			ObservedGeneration:  providerObj.GetGeneration(),
			ProviderUID:         providerObj.GetUID(),
			ID:                  pod.Name,
			Operations:          []string{},
			LastTransitionTime:  util.Now(),
			LastCacheUpdateTime: util.Now(),
		}

		g.Expect(k8sClient.Update(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that errors are properly reflected in provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider should have one pod status")
			g.Expect(len(providerObj.Status.ByPod[0].Errors)).Should(gomega.Equal(1), "Provider status should have one error")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "Provider should not be active due to error")

			// Check error details
			errors := providerObj.Status.ByPod[0].Errors
			g.Expect(string(errors[0].Type)).Should(gomega.Equal(string(statusv1beta1.UpsertCacheError)), "Second error should be query error")
			g.Expect(errors[0].Message).Should(gomega.Equal("Failed to update external data provider cache"))
			g.Expect(errors[0].Retryable).Should(gomega.BeFalse())
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// transition to no error
		providerPodStatusObj.Status = statusv1beta1.ProviderPodStatusStatus{
			Active:              true,
			Errors:              []*statusv1beta1.ProviderError{},
			ObservedGeneration:  providerObj.GetGeneration(),
			ProviderUID:         providerObj.GetUID(),
			ID:                  pod.Name,
			Operations:          []string{},
			LastTransitionTime:  util.Now(),
			LastCacheUpdateTime: util.Now(),
		}

		g.Expect(k8sClient.Update(ctx, &providerPodStatusObj)).Should(gomega.Succeed())

		// Await for reconciliation
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeProviderNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Check that errors are properly reflected in provider status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeProviderNamespacedName, &providerObj)
			g.Expect(err).Should(gomega.Succeed())
			g.Expect(len(providerObj.Status.ByPod)).Should(gomega.Equal(1), "Provider should have one pod status")
			g.Expect(len(providerObj.Status.ByPod[0].Errors)).Should(gomega.Equal(0), "Provider status should have no errors")
			g.Expect(providerObj.Status.ByPod[0].Active).Should(gomega.BeTrue(), "Provider should be active due to no errors")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup
		g.Expect(k8sClient.Delete(ctx, &providerPodStatusObj)).Should(gomega.Succeed())
		g.Expect(k8sClient.Delete(ctx, &providerObj)).Should(gomega.Succeed())
	})
}

func TestToProviderPodStatusStatus(t *testing.T) {
	now := metav1.Now()

	input := statusv1beta1.ProviderPodStatusStatus{
		ID:          "test-pod",
		ProviderUID: "test-provider-uid",
		Active:      false,
		Errors: []*statusv1beta1.ProviderError{
			{
				Type:           statusv1beta1.ConversionError,
				Message:        "Conversion failed",
				Retryable:      true,
				ErrorTimestamp: &now,
			},
			{
				Type:           statusv1beta1.UpsertCacheError,
				Message:        "Upsert failed",
				Retryable:      true,
				ErrorTimestamp: &now,
			},
		},
		Operations:          []string{"operation1", "operation2"},
		LastTransitionTime:  &now,
		LastCacheUpdateTime: &now,
		ObservedGeneration:  int64(1),
	}

	result := toProviderPodStatusStatus(&input)

	// Verify basic fields
	require.Equal(t, input.ID, result.ID)
	require.Equal(t, input.ProviderUID, result.ProviderUID)
	require.Equal(t, input.Active, result.Active)
	require.Equal(t, input.Operations, result.Operations)
	require.Equal(t, input.LastTransitionTime, result.LastTransitionTime)
	require.Equal(t, input.LastCacheUpdateTime, result.LastCacheUpdateTime)
	require.Equal(t, input.ObservedGeneration, result.ObservedGeneration)

	// Verify error conversion
	require.Equal(t, len(input.Errors), len(result.Errors))
	for i, inputErr := range input.Errors {
		resultErr := result.Errors[i]
		require.Equal(t, string(inputErr.Type), string(resultErr.Type))
		require.Equal(t, inputErr.Message, resultErr.Message)
		require.Equal(t, inputErr.Retryable, resultErr.Retryable)
		require.Equal(t, inputErr.ErrorTimestamp, resultErr.ErrorTimestamp)
	}
}

func TestSortableStatuses(t *testing.T) {
	statuses := sortableStatuses{
		{Status: statusv1beta1.ProviderPodStatusStatus{ID: "pod-c"}},
		{Status: statusv1beta1.ProviderPodStatusStatus{ID: "pod-a"}},
		{Status: statusv1beta1.ProviderPodStatusStatus{ID: "pod-b"}},
	}

	require.Equal(t, 3, statuses.Len())
	require.True(t, statuses.Less(1, 0))  // "pod-a" < "pod-c"
	require.False(t, statuses.Less(0, 1)) // "pod-c" > "pod-a"

	// Test swap
	statuses.Swap(0, 1)
	require.Equal(t, "pod-a", statuses[0].Status.ID)
	require.Equal(t, "pod-c", statuses[1].Status.ID)
}
