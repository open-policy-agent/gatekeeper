package connectionstatus

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	anythingtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
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

// Test operations on Connection and ConnectionPodStatus handled by controller and reflected on Connection status
func TestReconcile_E2E(t *testing.T) {
	// Setup
	const timeout = time.Second * 20
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}
	pod, _ := getPod(ctx)

	t.Run("Reconcile called and updates Connection status", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      *exportutil.AuditConnection,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: map[string]interface{}{
					"path":            "value",
					"maxAuditResults": float64(3),
				}},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      *exportutil.AuditConnection,
			Namespace: util.GetNamespace(),
		}

		// Wrap the controller Reconciler so it writes each request to a map when it is finished reconciling
		originalReconciler := newReconciler(mgr, *exportutil.AuditConnection)
		wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
		// Register the controller with the manager
		require.NoError(t, add(mgr, wrappedReconciler))
		// Start the manager and let it run in the background
		testutils.StartManager(ctx, t, mgr)

		// Test setup - Create the connection object
		g.Expect(k8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			// Use the Connection object for Reconcile request because of the Connection mapper
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Connection object should now exist
		connObj = connectionv1alpha1.Connection{}
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			g.Expect(err).Should(gomega.BeNil())
		}).WithTimeout(timeout).Should(gomega.Succeed(), "Connection object should exist after creation")

		// Next create the ConnectionPodStatus object which should trigger the reconcile request
		connPodStatusObjName, _ := statusv1alpha1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeConnectionPodStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusObjName,
			Namespace: util.GetNamespace(),
		}
		connPodStatusObj := statusv1alpha1.ConnectionPodStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      connPodStatusObjName,
				Namespace: util.GetNamespace(),
				Labels: map[string]string{
					statusv1beta1.ConnectionNameLabel: connObj.Name,
				},
			},
			Status: statusv1alpha1.ConnectionPodStatusStatus{
				Active:             false,
				Errors:             []*statusv1alpha1.ConnectionError{},
				ObservedGeneration: connObj.GetGeneration(),
				ConnectionUID:      connObj.GetUID(),
				ID:                 pod.Name,
			},
		}

		// Now create the connection pod status object which should trigger the reconcile request
		g.Expect(k8sClient.Create(ctx, &connPodStatusObj)).Should(gomega.Succeed(), "Creating the connection pod status object should succeed")

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			// Use the Connection object for Reconcile request because of the Connection mapper
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Assert ConnectionPodStatus object creation
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeConnectionPodStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObj.Status.Errors).Should(gomega.BeEmpty(), "Status should not have an error after creation")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
			g.Expect(connPodStatusObj.Status.Active).Should(gomega.BeFalse(), "No publish operations have been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Assert Connection object and its status
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			g.Expect(err).Should(gomega.Succeed(), "Conn should exist after updating the connection object")
			g.Expect(len(connObj.Status.ByPod)).Should(gomega.Equal(1), "Connection object status should have one entry")
			g.Expect(connObj.Status.ByPod[0].Errors).Should(gomega.BeEmpty(), "Status should not have an error after updating the connection object")
			g.Expect(connObj.Status.ByPod[0].ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should get updated to match the latest connection object generation after update")
			g.Expect(connObj.Status.ByPod[0].ID).Should(gomega.Equal(pod.Name), "ID should still match the pod name after update")
			g.Expect(connObj.Status.ByPod[0].ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should still match the connection object UID after update")
			g.Expect(connObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "No publish operations have been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Update of the Connection object
		connObj.Spec.Config = &anythingtypes.Anything{Value: map[string]interface{}{
			"path":            "new-value",
			"maxAuditResults": float64(3),
		}}
		g.Expect(k8sClient.Update(ctx, &connObj)).Should(gomega.Succeed(), "Updating the Connection object should succeed")

		// Also update the ConnectionPodStatus object to reflect the new generation
		connPodStatusObj.Status.ObservedGeneration = connObj.GetGeneration()
		g.Expect(k8sClient.Update(ctx, &connPodStatusObj)).Should(gomega.Succeed(), "Updating the ConnectionPodStatus object should succeed")

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Assert Connection status after update
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			g.Expect(err).Should(gomega.Succeed(), "Connection object should exist after updating")
			g.Expect(len(connObj.Status.ByPod)).Should(gomega.Equal(1), "Connection object status should have one entry")
			g.Expect(connObj.Status.ByPod[0].Errors).Should(gomega.BeEmpty(), "Status should not have an error after updating the Connection object")
			g.Expect(connObj.Status.ByPod[0].ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should get updated to match the latest connection object generation after update")
			g.Expect(connObj.Status.ByPod[0].ID).Should(gomega.Equal(pod.Name), "ID should still match the pod name after update")
			g.Expect(connObj.Status.ByPod[0].ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should still match the Connection object UID after update")
			g.Expect(connObj.Status.ByPod[0].Active).Should(gomega.BeFalse(), "No publish operations have been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Delete of the Connection object
		g.Expect(k8sClient.Delete(ctx, &connObj)).Should(gomega.Succeed(), "Deleting the connection object should succeed")
		// If Connection object deleted the pod status not necessarily deleted it wil persist
		g.Eventually(func() bool {
			err := k8sClient.Get(ctx, typeConnectionPodStatusNamespacedName, &connPodStatusObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(false), "Connection pod status object still exists even after Connection object deleted")

		// Cleanup the Connection and ConnectionPodStatus objects if they exist at the end
		defer func() {
			k8sClient.Delete(ctx, &connObj)          // nolint:errcheck
			k8sClient.Delete(ctx, &connPodStatusObj) // nolint:errcheck
		}()
	})
}
