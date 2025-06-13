package export

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const timeout = time.Second * 20

// Note: For this test we check the ConnectionPodStatus resource that is created
// by the controller, and not the Connection status itself, to isolate test boundaries
// since updating the Connection status is handled by a separate controller
func TestReconcile_E2E(t *testing.T) {
	// Setup
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export", "true"}))
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(ctx context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}

	t.Run("Reconcile called for new Connection create, then update, and finally delete all with expected operations and ConnectionPodStatus updates", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      *AuditConnection,
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
			Name:      *AuditConnection,
			Namespace: util.GetNamespace(),
		}

		// Wrap the controller Reconciler so it writes each request to a map when it is finished reconciling
		originalReconciler := newReconciler(mgr, export.NewSystem(), getPod)
		wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
		// Register the controller with the manager
		require.NoError(t, add(mgr, wrappedReconciler))
		// Start the manager and let it run in the background
		testutils.StartManager(ctx, t, mgr)

		// Cleanup the Connection object if it exists at the end
		defer func() {
			k8sClient.Delete(ctx, &connObj)
		}()

		// Connection object should not exist at the beginning of the test
		g.Expect(k8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)).ShouldNot(gomega.Succeed(), "Resource should not exist before creation")

		// Test setup create the Connection object
		g.Expect(k8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		// Assert ConnectionPodStatus
		connPodStatusObj := statusv1beta1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusName, _ := statusv1beta1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusName,
			Namespace: util.GetNamespace(),
		}
		var generationOnCreate int64
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObj.Status.Errors).Should(gomega.BeEmpty(), "Status should not have an error after creation")
			generationOnCreate = connObj.GetGeneration()
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
			g.Expect(connPodStatusObj.Status.Active).Should(gomega.BeFalse(), "No publish operations have been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Update of the connection object
		connObj.Spec.Config.Value = map[string]interface{}{
			"path":            "new-value",
			"maxAuditResults": float64(3),
		}
		g.Expect(k8sClient.Update(ctx, &connObj)).Should(gomega.Succeed(), "Updating the connection object should succeed")

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Reconcile request should finish after updating the connection object")

		// Assert the Connection object after the Connection update
		g.Eventually(func(g gomega.Gomega) {
			// Get the latest connection object
			err := k8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			g.Expect(err).Should(gomega.Succeed(), "Connection object should exist after update")
			g.Expect(connObj.Spec.Config.Value).Should(gomega.Equal(map[string]interface{}{"path": "new-value", "maxAuditResults": float64(3)}), "Connection object should have the updated config value after update")
			g.Expect(connObj.GetGeneration()).Should(gomega.Not(gomega.Equal(generationOnCreate)), "Connection object generation should have changed after update")
			g.Expect(connObj.Status.ByPod).Should(gomega.BeNil(), "Connection object status should be nil after update, as the controller does not set it")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Assert the ConnectionPodStatus after the Connection update
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should still exist after updating the connection object")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should still have the correct Connection name label after update")
			g.Expect(connPodStatusObj.Status.Errors).Should(gomega.BeEmpty(), "Status should not have an error after updating the connection object")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should get updated to match the latest Connection object generation after update")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).ShouldNot(gomega.Equal(generationOnCreate), "Observed generation should have changed after update")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should still match the pod name after update")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should still match the Connection object UID after update")
			g.Expect(connPodStatusObj.Status.Active).Should(gomega.BeFalse(), "No publish operations have been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Delete of the connection object
		g.Expect(k8sClient.Delete(ctx, &connObj)).Should(gomega.Succeed(), "Deleting the connection object should succeed")
		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Reconcile request should finish after deleting the connection object")

		// Assert the Connection and ConnectionPodStatus object after deleting the Connection object
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connObj)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection obj cleaned up after deleting the connection object")
			err = k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection pod status should get cleaned up after deleting the connection object")
		}).WithTimeout(timeout)
	})
}

// Mocks ExportSystem to simulate the export system behavior failures and impact on the controller
func TestReconcile_ExportSystem_Failures(t *testing.T) {
	// Setup
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export", "true"}))
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	getPod := func(ctx context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}

	t.Run("Reconcile called for Connection create, upsert fails, and status error", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      *AuditConnection,
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
			Name:      *AuditConnection,
			Namespace: util.GetNamespace(),
		}

		mockErrStr := "mock error for upsert connection"
		mockErr := fmt.Errorf("%s", mockErrStr)
		fakeExportSystem := &FakeExportSystem{
			UpsertConnectionError: mockErr,
		}

		directK8sClient, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme()})
		require.NoError(t, err, "Failed to create direct k8s client")
		reconciler := Reconciler{
			reader: directK8sClient,
			writer: directK8sClient,
			scheme: mgr.GetScheme(),
			system: fakeExportSystem,
			getPod: getPod,
		}

		// Test setup Create the connection object
		g.Expect(directK8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())
		// Cleanup the Connection object if it exists at the end
		defer func() {
			directK8sClient.Delete(ctx, &connObj)
		}()
		// Call Reconcile directly to assert the behavior on failures without having controller go through requeues
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeConnectionNamespacedName})
		// The system upsert error causes a requeue but the error doesn't get returned only the status update errors do
		g.Expect(result.Requeue).Should(gomega.Equal(true), "Reconcile should requeue after an error")
		g.Expect(err).Should(gomega.BeNil(), "Reconcile should not return an error on initial creation")

		// Assert the ConnectionPodStatus - Errors should be present after unsuccessful upsert
		connPodStatusObj := statusv1beta1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusName, _ := statusv1beta1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeConnPodStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusName,
			Namespace: util.GetNamespace(),
		}
		g.Eventually(func(g gomega.Gomega) {
			err := directK8sClient.Get(ctx, typeConnPodStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObj.Status.Errors[0].Message).Should(gomega.Equal(mockErrStr), "Status should have an error with expected message after creation")
			g.Expect(connPodStatusObj.Status.Errors[0].Type).Should(gomega.Equal(statusv1beta1.UpsertConnectionError), "Status should have an error with expected type after creation")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
			g.Expect(connPodStatusObj.Status.Active).Should(gomega.BeFalse(), "No publish operations has been performed yet, so active status should be false")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		g.Expect(fakeExportSystem.UpsertConnectionCalledCount).Should(gomega.Equal(1), "UpsertConnection count")
		g.Expect(fakeExportSystem.CloseConnectionCalledCount).Should(gomega.Equal(0), "CloseConnection count")
		g.Expect(fakeExportSystem.PublishCalledCount).Should(gomega.Equal(0), "Publish count")

		// Delete which should trigger CloseConnection and assert the behavior even on closed connection failures
		g.Expect(directK8sClient.Delete(ctx, &connObj)).Should(gomega.Succeed())
		mockErrStr = "mock error for close connection"
		mockErr = fmt.Errorf("%s", mockErrStr)
		fakeExportSystem = &FakeExportSystem{
			CloseConnectionError: mockErr,
		}
		reconciler.system = fakeExportSystem
		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeConnectionNamespacedName})
		// The system connection error causes a requeue but the error doesn't get returned only the status update errors do
		g.Expect(result.Requeue).Should(gomega.Equal(true), "Reconcile should requeue after an error")
		g.Expect(err).Should(gomega.BeNil(), "Reconcile should not return an error on initial creation")
		g.Expect(fakeExportSystem.UpsertConnectionCalledCount).Should(gomega.Equal(0), "UpsertConnection count")
		g.Expect(fakeExportSystem.CloseConnectionCalledCount).Should(gomega.Equal(1), "CloseConnection count")
		g.Expect(fakeExportSystem.PublishCalledCount).Should(gomega.Equal(0), "Publish count")

		// Assert the Connection object
		g.Eventually(func(g gomega.Gomega) bool {
			err := directK8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(true), "Resource should not exist after deletion")

		// Assert the ConnectionPodStatus object
		g.Eventually(func(g gomega.Gomega) bool {
			err := directK8sClient.Get(ctx, typeConnectionNamespacedName, &connPodStatusObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(true), "Resource should not exist after deletion")
	})
}

// Mock K8s client to simulate the client failures and impact on the controller
func TestReconcile_Client_Failures(t *testing.T) {
	// Setup
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export", "true"}))
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	getPod := func(ctx context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}

	t.Run("Test GET returns error causes requeue", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      *AuditConnection,
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
			Name:      *AuditConnection,
			Namespace: util.GetNamespace(),
		}

		mockErrStr := "mock error for upsert connection"
		mockErr := fmt.Errorf("%s", mockErrStr)
		fakeExportSystem := &FakeExportSystem{
			UpsertConnectionError: mockErr,
		}

		directK8sClient, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme()})
		require.NoError(t, err, "Failed to create direct k8s client")
		mockErr = fmt.Errorf("mock get error")
		fakeClient := &FakeClient{
			Client: directK8sClient,
			getErr: mockErr,
		}
		reconciler := Reconciler{
			reader: fakeClient,
			writer: fakeClient,
			scheme: mgr.GetScheme(),
			system: fakeExportSystem,
			getPod: getPod,
		}

		// Test setup - delete Connection object to ensure not already exist at the beginning of the test
		directK8sClient.Delete(ctx, &connObj)
		g.Eventually(func(g gomega.Gomega) bool {
			err := directK8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(true), "Resource should not exist before creation")

		// Test setup Create the Connection object
		g.Expect(directK8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())

		// Call Reconcile directly to assert the behavior on failures without having controller go through requeues
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeConnectionNamespacedName})
		g.Expect(result.Requeue).Should(gomega.Equal(false), "Reconcile should not requeue after the GET error")
		g.Expect(err).Should(gomega.Equal(mockErr), "Reconcile should return an error")
	})
}

type FakeClient struct {
	client.Client

	getErr    error
	updateErr error
	deleteErr error
	createErr error
}

func (f *FakeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if f.getErr != nil {
		return f.getErr
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func (f *FakeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	return f.Client.Update(ctx, obj, opts...)
}

func (f *FakeClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.Client.Delete(ctx, obj, opts...)
}

func (f *FakeClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if f.createErr != nil {
		return f.createErr
	}
	return f.Client.Create(ctx, obj, opts...)
}

type FakeExportSystem struct {
	PublishCalledCount          int
	PublishError                error
	UpsertConnectionCalledCount int
	UpsertConnectionError       error
	CloseConnectionCalledCount  int
	CloseConnectionError        error
}

func (f *FakeExportSystem) Publish(ctx context.Context, connectionName string, subject string, msg interface{}) error {
	f.PublishCalledCount++
	if f.PublishError != nil {
		return f.PublishError
	}
	return nil
}

func (f *FakeExportSystem) UpsertConnection(ctx context.Context, config interface{}, connectionName string, newDriver string) error {
	f.UpsertConnectionCalledCount++
	if f.UpsertConnectionError != nil {
		return f.UpsertConnectionError
	}
	return nil
}

func (f *FakeExportSystem) CloseConnection(connectionName string) error {
	f.CloseConnectionCalledCount++
	if f.CloseConnectionError != nil {
		return f.CloseConnectionError
	}
	return nil
}
