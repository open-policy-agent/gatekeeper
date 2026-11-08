package export

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/onsi/gomega"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	anythingtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Test notes - we use a separate audit connection name for each test to avoid race conditions
// between tests sharing the same testenv etcd objects that with the same audit connection name would otherwise cause conflicts

const timeout = time.Second * 20

type countingClient struct {
	client.Client
	creates int
	updates int
}

func (c *countingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.creates++
	return c.Client.Create(ctx, obj, opts...)
}

func (c *countingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates++
	return c.Client.Update(ctx, obj, opts...)
}

func TestAdderAddRejectsMissingExportSystemWhenExportEnabled(t *testing.T) {
	origExport := *exportutil.ExportEnabled
	defer func() { *exportutil.ExportEnabled = origExport }()
	*exportutil.ExportEnabled = true

	a := &Adder{ExportSystem: nil}
	require.Error(t, a.Add(nil))
}

// TestAdderAddRejectsTypedNilExportSystemWhenExportEnabled guards against the
// typed-nil interface footgun: main.go injects ExportSystem as a concrete
// *export.System, so a nil *export.System wrapped in the export.Exporter
// interface must still be rejected, not just a literal nil interface.
func TestAdderAddRejectsTypedNilExportSystemWhenExportEnabled(t *testing.T) {
	origExport := *exportutil.ExportEnabled
	defer func() { *exportutil.ExportEnabled = origExport }()
	*exportutil.ExportEnabled = true

	var typedNilSystem *export.System
	a := &Adder{ExportSystem: typedNilSystem}
	require.Error(t, a.Add(nil))
}

// A pod without the audit or webhook operation must not register the
// controller, even with both export flags on and no export system.
func TestAdderAddSkipsProcessWithoutExportOperation(t *testing.T) {
	origExport := *exportutil.ExportEnabled
	origAdmission := *exportutil.AdmissionExportEnabled
	defer func() {
		*exportutil.ExportEnabled = origExport
		*exportutil.AdmissionExportEnabled = origAdmission
	}()
	*exportutil.ExportEnabled = true
	*exportutil.AdmissionExportEnabled = true
	restore, err := operations.SetForTest(operations.Status)
	require.NoError(t, err)
	defer restore()

	a := &Adder{ExportSystem: nil}
	require.NoError(t, a.Add(nil))
}

// TestIsNilExporterDoesNotPanicOnValueTypeExportSystem guards against a
// reflect.Value.IsNil panic: value-type Exporter implementations (e.g. a
// struct, as opposed to a pointer) are not nilable, so the nil check must not
// call IsNil on them.
func TestIsNilExporterDoesNotPanicOnValueTypeExportSystem(t *testing.T) {
	var got bool
	require.NotPanics(t, func() {
		got = isNilExporter(valueExportSystem{})
	})
	require.False(t, got)
}

// valueExportSystem is a value-type (non-pointer) implementation of
// export.Exporter, used to exercise the case where reflect.Value.IsNil
// cannot be called since the underlying kind is not nilable.
type valueExportSystem struct{}

func (valueExportSystem) Publish(_ context.Context, _ string, _ string, _ interface{}) error {
	return nil
}

func (valueExportSystem) UpsertConnection(_ context.Context, _ interface{}, _ string, _ string) error {
	return nil
}

func (valueExportSystem) CloseConnection(_ string) error {
	return nil
}

func TestUpdateOrCreateConnectionPodStatusSkipsStableSecondUpdate(t *testing.T) {
	ctx := context.Background()
	pod := fakes.Pod(fakes.WithNamespace(util.GetNamespace()), fakes.WithName("status-pod"), fakes.WithUID("status-pod-uid"))
	connObj := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "audit-connection-skip-stable",
			Namespace:  util.GetNamespace(),
			UID:        "connection-uid",
			Generation: 1,
		},
	}
	k8sClient := &countingClient{Client: fake.NewClientBuilder().WithScheme(k8sscheme.Scheme).Build()}
	getPod := func(context.Context) (*corev1.Pod, error) { return pod, nil }

	require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, connObj, nil, nil, getPod))
	require.Equal(t, 1, k8sClient.creates)
	require.Equal(t, 0, k8sClient.updates)

	require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, connObj, nil, nil, getPod))
	require.Equal(t, 1, k8sClient.creates)
	require.Equal(t, 0, k8sClient.updates)
}

func TestUpdateOrCreateConnectionPodStatusRepairsMetadata(t *testing.T) {
	ctx := context.Background()
	pod := fakes.Pod(fakes.WithNamespace(util.GetNamespace()), fakes.WithName("status-pod"), fakes.WithUID("status-pod-uid"))
	connObj := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "audit-connection-repair-metadata",
			Namespace:  util.GetNamespace(),
			UID:        "connection-uid",
			Generation: 1,
		},
	}
	status, err := statusv1alpha1.NewConnectionStatusForPod(pod, connObj.GetNamespace(), connObj.GetName(), k8sscheme.Scheme)
	require.NoError(t, err)
	status.SetLabels(nil)
	status.SetOwnerReferences(nil)
	status.Status.ConnectionUID = connObj.GetUID()
	status.Status.ObservedGeneration = connObj.GetGeneration()

	k8sClient := &countingClient{Client: fake.NewClientBuilder().WithScheme(k8sscheme.Scheme).WithObjects(status).Build()}
	getPod := func(context.Context) (*corev1.Pod, error) { return pod, nil }

	require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, connObj, nil, nil, getPod))
	require.Equal(t, 0, k8sClient.creates)
	require.Equal(t, 1, k8sClient.updates)

	got := &statusv1alpha1.ConnectionPodStatus{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(status), got))
	require.Equal(t, connObj.GetName(), got.Labels[statusv1beta1.ConnectionNameLabel])
	require.Equal(t, pod.GetName(), got.Labels[statusv1beta1.PodLabel])
	require.Len(t, got.OwnerReferences, 1)
}

func TestUpdateOrCreateConnectionPodStatusResetsStaleConnectionUID(t *testing.T) {
	ctx := context.Background()
	pod := fakes.Pod(fakes.WithNamespace(util.GetNamespace()), fakes.WithName("status-pod"), fakes.WithUID("status-pod-uid"))
	connObj := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "audit-connection-recreated",
			Namespace:  util.GetNamespace(),
			UID:        "new-connection-uid",
			Generation: 1,
		},
	}
	status, err := statusv1alpha1.NewConnectionStatusForPod(pod, connObj.GetNamespace(), connObj.GetName(), k8sscheme.Scheme)
	require.NoError(t, err)
	status.Status.ConnectionUID = "old-connection-uid"
	status.Status.ObservedGeneration = connObj.GetGeneration()
	status.Status.ConnectionErrors = []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: "old error"}}
	status.Status.PublishStatuses = []statusv1alpha1.ConnectionPublishStatus{{Source: statusv1alpha1.WebhookPublishSource, Active: true}}

	k8sClient := fake.NewClientBuilder().WithScheme(k8sscheme.Scheme).WithObjects(status).Build()
	getPod := func(context.Context) (*corev1.Pod, error) { return pod, nil }

	require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, connObj, nil, nil, getPod))

	got := &statusv1alpha1.ConnectionPodStatus{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKeyFromObject(status), got))
	require.Equal(t, connObj.GetUID(), got.Status.ConnectionUID)
	require.Empty(t, got.Status.ConnectionErrors)
	require.Empty(t, got.Status.PublishStatuses)
}

func diskConnectionConfig(path string) map[string]interface{} {
	return map[string]interface{}{
		"path":            path,
		"maxAuditResults": float64(3),
	}
}

func TestSetConnectionPublishStatusPreservesOtherSources(t *testing.T) {
	auditAttempt := metav1.NewTime(time.Unix(1, 0))
	webhookAttempt := metav1.NewTime(time.Unix(2, 0))
	webhookSuccess := metav1.NewTime(time.Unix(2, 0))
	status := statusv1alpha1.ConnectionPodStatusStatus{
		PublishStatuses: []statusv1alpha1.ConnectionPublishStatus{
			{
				Source:          statusv1alpha1.AuditPublishSource,
				LastAttemptTime: &auditAttempt,
				Errors: []*statusv1alpha1.ConnectionError{
					{Type: statusv1alpha1.PublishError, Message: "audit unavailable"},
				},
			},
		},
	}

	setConnectionPublishStatus(&status, statusv1alpha1.ConnectionPublishStatus{
		Source:          statusv1alpha1.WebhookPublishSource,
		Active:          true,
		LastAttemptTime: &webhookAttempt,
		LastSuccessTime: &webhookSuccess,
	})

	require.Equal(t, []statusv1alpha1.ConnectionPublishSource{
		statusv1alpha1.AuditPublishSource,
		statusv1alpha1.WebhookPublishSource,
	}, []statusv1alpha1.ConnectionPublishSource{
		status.PublishStatuses[0].Source,
		status.PublishStatuses[1].Source,
	})
	require.Equal(t, []*statusv1alpha1.ConnectionError{
		{Type: statusv1alpha1.PublishError, Message: "audit unavailable"},
	}, status.PublishStatuses[0].Errors)
	require.True(t, status.PublishStatuses[1].Active)

	auditSuccess := metav1.NewTime(time.Unix(3, 0))
	setConnectionPublishStatus(&status, statusv1alpha1.ConnectionPublishStatus{
		Source:          statusv1alpha1.AuditPublishSource,
		Active:          true,
		LastAttemptTime: &auditSuccess,
		LastSuccessTime: &auditSuccess,
	})

	require.Empty(t, status.PublishStatuses[0].Errors)
	require.True(t, status.PublishStatuses[1].Active)
}

func TestConnectionStatusErrorsAreCapped(t *testing.T) {
	errors := make([]*statusv1alpha1.ConnectionError, exportutil.MaxConnectionStatusErrors+1)
	for i := range errors {
		errors[i] = &statusv1alpha1.ConnectionError{Type: statusv1alpha1.PublishError, Message: fmt.Sprintf("error-%d", i)}
	}
	status := statusv1alpha1.ConnectionPodStatusStatus{}

	setConnectionPublishStatus(&status, statusv1alpha1.ConnectionPublishStatus{
		Source: statusv1alpha1.WebhookPublishSource,
		Errors: errors,
	})
	require.Len(t, status.PublishStatuses[0].Errors, exportutil.MaxConnectionStatusErrors)

	setConnectionErrors(&status, errors)
	require.Len(t, status.ConnectionErrors, exportutil.MaxConnectionStatusErrors)
}

func TestConnectionPodStatusUpdateRequiresReconcile(t *testing.T) {
	oldStatus := &statusv1alpha1.ConnectionPodStatus{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"connection": "audit"}},
		Status: statusv1alpha1.ConnectionPodStatusStatus{
			ObservedGeneration: 1,
			PublishStatuses: []statusv1alpha1.ConnectionPublishStatus{
				{Source: statusv1alpha1.WebhookPublishSource, Active: true},
			},
		},
	}
	newStatus := oldStatus.DeepCopy()
	newStatus.Status.PublishStatuses[0].Active = false
	require.False(t, connectionPodStatusUpdateRequiresReconcile(oldStatus, newStatus))

	newStatus.Status.ConnectionErrors = []*statusv1alpha1.ConnectionError{
		{Type: statusv1alpha1.UpsertConnectionError, Message: "unavailable"},
	}
	require.True(t, connectionPodStatusUpdateRequiresReconcile(oldStatus, newStatus))

	newStatus = oldStatus.DeepCopy()
	newStatus.Labels["connection"] = "other"
	require.True(t, connectionPodStatusUpdateRequiresReconcile(oldStatus, newStatus))
}

func TestConnectionUpdateRequiresReconcile(t *testing.T) {
	oldConn := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Generation: 1, UID: "uid-1"},
		Spec:       connectionv1alpha1.ConnectionSpec{Driver: disk.Name},
	}

	newConn := oldConn.DeepCopy()
	newConn.Status.ByPod = []statusv1alpha1.ConnectionPodStatusStatus{{ID: "audit"}}
	require.False(t, connectionUpdateRequiresReconcile(oldConn, newConn), "status-only update")

	newConn = oldConn.DeepCopy()
	newConn.Labels = map[string]string{"team": "a"}
	newConn.Annotations = map[string]string{"note": "b"}
	require.False(t, connectionUpdateRequiresReconcile(oldConn, newConn), "metadata-only update")

	newConn = oldConn.DeepCopy()
	newConn.Generation = 2
	require.True(t, connectionUpdateRequiresReconcile(oldConn, newConn), "spec update")

	newConn = oldConn.DeepCopy()
	newConn.UID = "uid-2"
	require.True(t, connectionUpdateRequiresReconcile(oldConn, newConn), "recreated at the same generation")

	require.True(t, connectionUpdateRequiresReconcile(nil, newConn))
	require.True(t, connectionUpdateRequiresReconcile(oldConn, nil))
}

type conflictOnceWriter struct {
	client.Client
	onConflict func(context.Context) error
	once       sync.Once
}

func (writer *conflictOnceWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	conflict := false
	var conflictErr error
	writer.once.Do(func() {
		conflict = true
		conflictErr = writer.onConflict(ctx)
	})
	if conflictErr != nil {
		return conflictErr
	}
	if conflict {
		return apierrors.NewConflict(
			schema.GroupResource{Group: "status.gatekeeper.sh", Resource: "connectionpodstatuses"},
			obj.GetName(),
			fmt.Errorf("concurrent publish status update"),
		)
	}
	return writer.Client.Update(ctx, obj, opts...)
}

func TestUpdateConnectionPodPublishStatusClassifiesMissingConnection(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, connectionv1alpha1.AddToScheme(testScheme))
	require.NoError(t, statusv1alpha1.AddToScheme(testScheme))
	require.NoError(t, corev1.AddToScheme(testScheme))
	connection := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "connection", Namespace: util.GetNamespace()},
	}
	podNotFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "audit-pod")

	for _, missingConnection := range []bool{true, false} {
		t.Run(fmt.Sprintf("missing connection=%t", missingConnection), func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(testScheme)
			if !missingConnection {
				builder.WithObjects(connection)
			}
			k8sClient := builder.Build()
			err := UpdateConnectionPodPublishStatus(
				context.Background(), k8sClient, k8sClient, testScheme, connection.Name,
				statusv1alpha1.ConnectionPublishStatus{Source: statusv1alpha1.AuditPublishSource},
				func(context.Context) (*corev1.Pod, error) { return nil, podNotFound },
			)
			require.Error(t, err)
			require.True(t, apierrors.IsNotFound(err), "preserve Kubernetes error classification")
			require.Equal(t, missingConnection, errors.Is(err, ErrConnectionNotFound))
			if !missingConnection {
				require.ErrorIs(t, err, podNotFound)
			}
		})
	}
}

func TestUpdateConnectionPodPublishStatusMergesAfterConflict(t *testing.T) {
	testutils.Setenv(t, "POD_NAMESPACE", "gatekeeper-system")
	testScheme := runtime.NewScheme()
	require.NoError(t, connectionv1alpha1.AddToScheme(testScheme))
	require.NoError(t, statusv1alpha1.AddToScheme(testScheme))
	require.NoError(t, corev1.AddToScheme(testScheme))

	connection := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "connection",
			Namespace:  util.GetNamespace(),
			UID:        types.UID("connection-uid"),
			Generation: 1,
		},
	}
	pod := fakes.Pod(fakes.WithNamespace(util.GetNamespace()), fakes.WithName("gatekeeper-pod"))
	statusName, err := statusv1alpha1.KeyForConnection(pod.Name, connection.Namespace, connection.Name)
	require.NoError(t, err)
	connectionStatus := &statusv1alpha1.ConnectionPodStatus{
		ObjectMeta: metav1.ObjectMeta{Name: statusName, Namespace: util.GetNamespace()},
		Status: statusv1alpha1.ConnectionPodStatusStatus{
			ID:                 pod.Name,
			ConnectionUID:      connection.UID,
			ObservedGeneration: connection.Generation,
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(connection, connectionStatus).Build()
	writer := &conflictOnceWriter{
		Client: k8sClient,
		onConflict: func(ctx context.Context) error {
			latest := &statusv1alpha1.ConnectionPodStatus{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(connectionStatus), latest); err != nil {
				return err
			}
			setConnectionPublishStatus(&latest.Status, statusv1alpha1.ConnectionPublishStatus{
				Source: statusv1alpha1.AuditPublishSource,
				Errors: []*statusv1alpha1.ConnectionError{
					{Type: statusv1alpha1.PublishError, Message: "audit unavailable"},
				},
			})
			return k8sClient.Update(ctx, latest)
		},
	}

	err = UpdateConnectionPodPublishStatus(
		context.Background(),
		k8sClient,
		writer,
		testScheme,
		connection.Name,
		statusv1alpha1.ConnectionPublishStatus{Source: statusv1alpha1.WebhookPublishSource, Active: true},
		func(context.Context) (*corev1.Pod, error) { return pod, nil },
	)
	require.NoError(t, err)

	latest := &statusv1alpha1.ConnectionPodStatus{}
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKeyFromObject(connectionStatus), latest))
	require.Equal(t, []statusv1alpha1.ConnectionPublishSource{
		statusv1alpha1.AuditPublishSource,
		statusv1alpha1.WebhookPublishSource,
	}, []statusv1alpha1.ConnectionPublishSource{
		latest.Status.PublishStatuses[0].Source,
		latest.Status.PublishStatuses[1].Source,
	})
	require.Equal(t, []*statusv1alpha1.ConnectionError{
		{Type: statusv1alpha1.PublishError, Message: "audit unavailable"},
	}, latest.Status.PublishStatuses[0].Errors)
	require.True(t, latest.Status.PublishStatuses[1].Active)

	connection.Generation++
	require.NoError(t, k8sClient.Update(context.Background(), connection))
	require.NoError(t, UpdateConnectionPodPublishStatus(
		context.Background(),
		k8sClient,
		k8sClient,
		testScheme,
		connection.Name,
		statusv1alpha1.ConnectionPublishStatus{Source: statusv1alpha1.WebhookPublishSource},
		func(context.Context) (*corev1.Pod, error) { return pod, nil },
	))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKeyFromObject(connectionStatus), latest))
	require.Equal(t, connection.Generation, latest.Status.ObservedGeneration)
	require.Equal(t, []statusv1alpha1.ConnectionPublishStatus{
		{Source: statusv1alpha1.WebhookPublishSource},
	}, latest.Status.PublishStatuses)
	require.Empty(t, latest.Status.ConnectionErrors)

	require.NoError(t, UpdateConnectionPodPublishStatus(
		context.Background(),
		k8sClient,
		k8sClient,
		testScheme,
		connection.Name,
		statusv1alpha1.ConnectionPublishStatus{Source: statusv1alpha1.AuditPublishSource, Active: true},
		func(context.Context) (*corev1.Pod, error) { return pod, nil },
	))
	require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKeyFromObject(connectionStatus), latest))
	require.Equal(t, []statusv1alpha1.ConnectionPublishSource{
		statusv1alpha1.AuditPublishSource,
		statusv1alpha1.WebhookPublishSource,
	}, []statusv1alpha1.ConnectionPublishSource{
		latest.Status.PublishStatuses[0].Source,
		latest.Status.PublishStatuses[1].Source,
	})
	require.True(t, latest.Status.PublishStatuses[0].Active)
}

// Note: For this test we check the ConnectionPodStatus resource that is created
// by the controller, and not the Connection status itself, to isolate test boundaries
// since updating the Connection status is handled by a separate controller.
func TestReconcile_E2E(t *testing.T) {
	// Setup
	auditConnectionName := "audit-connection-1"
	auditConnectionNameFlag := fmt.Sprintf("--audit-connection=%s", auditConnectionName)
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", auditConnectionNameFlag}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}
	// Wrap the controller Reconciler so it writes each request to a map when it is finished reconciling
	originalReconciler := newReconciler(mgr, export.NewSystem(), auditConnectionName, getPod)
	wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
	// Register the controller with the manager
	require.NoError(t, add(mgr, wrappedReconciler, originalReconciler.auditConnectionName))
	// Start the manager and let it run in the background
	testutils.StartManager(ctx, t, mgr)

	t.Run("Reconcile called for new Connection create, then update, and finally delete, all with expected operations and ConnectionPodStatus updates", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionName,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      auditConnectionName,
			Namespace: util.GetNamespace(),
		}

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
		connPodStatusObj := statusv1alpha1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusName, _ := statusv1alpha1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusName,
			Namespace: util.GetNamespace(),
		}
		var generationOnCreate int64
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObj.Status.ConnectionErrors).Should(gomega.BeEmpty(), "Status should not have an error after creation")
			generationOnCreate = connObj.GetGeneration()
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
			g.Expect(connPodStatusObj.Status.PublishStatuses).Should(gomega.BeEmpty(), "No publish operations have been performed yet")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Test Update of the connection object
		updatedConfig := diskConnectionConfig(t.TempDir())
		connObj.Spec.Config.Value = updatedConfig
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
			g.Expect(connObj.Spec.Config.Value).Should(gomega.Equal(updatedConfig), "Connection object should have the updated config value after update")
			g.Expect(connObj.GetGeneration()).Should(gomega.Not(gomega.Equal(generationOnCreate)), "Connection object generation should have changed after update")
			g.Expect(connObj.Status.ByPod).Should(gomega.BeNil(), "Connection object status should be nil after update, as the controller does not set it")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Assert the ConnectionPodStatus after the Connection update
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should still exist after updating the connection object")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should still have the correct Connection name label after update")
			g.Expect(connPodStatusObj.Status.ConnectionErrors).Should(gomega.BeEmpty(), "Status should not have an error after updating the connection object")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should get updated to match the latest Connection object generation after update")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).ShouldNot(gomega.Equal(generationOnCreate), "Observed generation should have changed after update")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should still match the pod name after update")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should still match the Connection object UID after update")
			g.Expect(connPodStatusObj.Status.PublishStatuses).Should(gomega.BeEmpty(), "Publish status should reset for the new Connection generation")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Clear the previous request with the same name to avoid false positives now only load the latest
		requests.Clear()

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

		// Cleanup the Connection object if it exists at the end
		defer func() {
			k8sClient.Delete(ctx, &connObj)          // nolint:errcheck
			k8sClient.Delete(ctx, &connPodStatusObj) // nolint:errcheck
		}()
	})
}

// Status and metadata updates must not re-run UpsertConnection.
func TestReconcile_IgnoresConnectionStatusAndMetadataUpdates(t *testing.T) {
	auditConnectionName := "audit-connection-5"
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", "--audit-connection=" + auditConnectionName}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		return fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod")), nil
	}
	originalReconciler := newReconciler(mgr, export.NewSystem(), auditConnectionName, getPod)
	var reconciles atomic.Int32
	countingReconciler := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		if req.Name == auditConnectionName {
			reconciles.Add(1)
		}
		return originalReconciler.Reconcile(ctx, req)
	})
	require.NoError(t, add(mgr, countingReconciler, auditConnectionName))
	testutils.StartManager(ctx, t, mgr)

	key := types.NamespacedName{Name: auditConnectionName, Namespace: util.GetNamespace()}
	connObj := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec: connectionv1alpha1.ConnectionSpec{
			Driver: disk.Name,
			Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
		},
	}
	g.Expect(k8sClient.Create(ctx, connObj)).Should(gomega.Succeed())
	t.Cleanup(func() { _ = k8sClient.Delete(context.Background(), connObj) })

	pod, _ := getPod(ctx)
	statusName, err := statusv1alpha1.KeyForConnection(pod.Name, key.Namespace, key.Name)
	require.NoError(t, err)
	statusKey := types.NamespacedName{Name: statusName, Namespace: util.GetNamespace()}
	waitForObservedGeneration := func(generation int64) {
		g.Eventually(func(g gomega.Gomega) {
			podStatus := &statusv1alpha1.ConnectionPodStatus{}
			g.Expect(k8sClient.Get(ctx, statusKey, podStatus)).Should(gomega.Succeed())
			g.Expect(podStatus.Status.ObservedGeneration).Should(gomega.Equal(generation))
			g.Expect(podStatus.Status.ConnectionErrors).Should(gomega.BeEmpty())
		}).WithTimeout(timeout).Should(gomega.Succeed())
	}
	waitForObservedGeneration(connObj.GetGeneration())
	g.Expect(connectionState(originalReconciler.state.Load())).Should(gomega.Equal(stateReady))

	// Let the create, status create, and seed reconciles drain.
	g.Eventually(func() bool {
		before := reconciles.Load()
		time.Sleep(time.Second)
		return reconciles.Load() == before
	}).WithTimeout(timeout).Should(gomega.BeTrue())
	settled := reconciles.Load()

	waitForCache := func(matches func(*connectionv1alpha1.Connection) bool) {
		g.Eventually(func() bool {
			cached := &connectionv1alpha1.Connection{}
			return mgr.GetCache().Get(ctx, key, cached) == nil && matches(cached)
		}).WithTimeout(timeout).Should(gomega.BeTrue())
	}
	for _, id := range []string{"audit-run-1", "audit-run-2"} {
		g.Eventually(func() error {
			latest := &connectionv1alpha1.Connection{}
			if err := k8sClient.Get(ctx, key, latest); err != nil {
				return err
			}
			latest.Status.ByPod = []statusv1alpha1.ConnectionPodStatusStatus{{ID: id, ObservedGeneration: latest.GetGeneration()}}
			return mgr.GetClient().Status().Update(ctx, latest)
		}).WithTimeout(timeout).Should(gomega.Succeed())
		waitForCache(func(cached *connectionv1alpha1.Connection) bool {
			return len(cached.Status.ByPod) == 1 && cached.Status.ByPod[0].ID == id
		})
	}
	g.Eventually(func() error {
		latest := &connectionv1alpha1.Connection{}
		if err := k8sClient.Get(ctx, key, latest); err != nil {
			return err
		}
		latest.Labels = map[string]string{"team": "platform"}
		return mgr.GetClient().Update(ctx, latest)
	}).WithTimeout(timeout).Should(gomega.Succeed())
	waitForCache(func(cached *connectionv1alpha1.Connection) bool {
		return cached.Labels["team"] == "platform"
	})
	g.Consistently(reconciles.Load).WithTimeout(3*time.Second).Should(gomega.Equal(settled), "status and metadata updates must not reconcile")

	// A spec change still reconciles.
	var updatedGeneration int64
	g.Eventually(func() error {
		latest := &connectionv1alpha1.Connection{}
		if err := k8sClient.Get(ctx, key, latest); err != nil {
			return err
		}
		latest.Spec.Config = &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())}
		if err := mgr.GetClient().Update(ctx, latest); err != nil {
			return err
		}
		updatedGeneration = latest.GetGeneration()
		return nil
	}).WithTimeout(timeout).Should(gomega.Succeed())
	g.Expect(updatedGeneration).Should(gomega.BeNumerically(">", connObj.GetGeneration()))
	g.Eventually(reconciles.Load).WithTimeout(timeout).Should(gomega.BeNumerically(">", settled))
	waitForObservedGeneration(updatedGeneration)
}

// Mocks ExportSystem to simulate the export system behavior failures and impact on the controller.
func TestReconcile_ExportSystem_Failures(t *testing.T) {
	// Setup
	auditConnectionName := "audit-connection-2"
	auditConnectionNameFlag := fmt.Sprintf("--audit-connection=%s", auditConnectionName)
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", auditConnectionNameFlag}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}

	t.Run("Reconcile called for Connection create, upsert fails, and status error", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionName,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      auditConnectionName,
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
			reader:              directK8sClient,
			writer:              directK8sClient,
			scheme:              mgr.GetScheme(),
			system:              fakeExportSystem,
			auditConnectionName: auditConnectionName,
			getPod:              getPod,
		}

		// Test setup Create the connection object
		g.Expect(directK8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())

		// Call Reconcile directly to assert the behavior on failures without having controller go through requeues
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeConnectionNamespacedName})
		// The system upsert error causes a requeue but the error doesn't get returned only the status update errors do
		g.Expect(result.Requeue).Should(gomega.Equal(true), "Reconcile should requeue after an error") // nolint:staticcheck
		g.Expect(err).Should(gomega.BeNil(), "Reconcile should not return an error on initial creation")

		// Assert the ConnectionPodStatus - Errors should be present after unsuccessful upsert
		connPodStatusObj := statusv1alpha1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusName, _ := statusv1alpha1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeConnPodStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusName,
			Namespace: util.GetNamespace(),
		}
		g.Eventually(func(g gomega.Gomega) {
			err := directK8sClient.Get(ctx, typeConnPodStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObj.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObj.Status.ConnectionErrors[0].Message).Should(gomega.Equal(mockErrStr), "Status should have an error with expected message after creation")
			g.Expect(connPodStatusObj.Status.ConnectionErrors[0].Type).Should(gomega.Equal(statusv1alpha1.UpsertConnectionError), "Status should have an error with expected type after creation")
			g.Expect(connPodStatusObj.Status.ObservedGeneration).Should(gomega.Equal(connObj.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObj.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObj.Status.ConnectionUID).Should(gomega.Equal(connObj.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
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
		g.Expect(result.Requeue).Should(gomega.Equal(true), "Reconcile should requeue after an error") // nolint:staticcheck
		g.Expect(err).Should(gomega.BeNil(), "Reconcile should not return an error on initial creation")
		g.Expect(fakeExportSystem.UpsertConnectionCalledCount).Should(gomega.Equal(0), "UpsertConnection count")
		g.Expect(fakeExportSystem.CloseConnectionCalledCount).Should(gomega.Equal(1), "CloseConnection count")
		g.Expect(fakeExportSystem.PublishCalledCount).Should(gomega.Equal(0), "Publish count")

		// Assert the Connection object
		g.Eventually(func() bool {
			err := directK8sClient.Get(ctx, typeConnectionNamespacedName, &connObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(true), "Resource should not exist after deletion")

		// Assert the ConnectionPodStatus object
		g.Eventually(func() bool {
			err := directK8sClient.Get(ctx, typeConnectionNamespacedName, &connPodStatusObj)
			if err != nil && apierrors.IsNotFound(err) {
				return true
			}
			return false
		}).WithTimeout(timeout).Should(gomega.Equal(true), "Resource should not exist after deletion")

		// Cleanup the Connection object if it exists at the end
		defer func() {
			directK8sClient.Delete(ctx, &connObj)          // nolint:errcheck
			directK8sClient.Delete(ctx, &connPodStatusObj) // nolint:errcheck
		}()
	})
}

// A Connection without spec is valid in the CRD and must not panic the controller.
func TestReconcile_ConnectionWithoutSpec(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	directK8sClient, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme()})
	require.NoError(t, err)

	connectionName := "audit-connection-no-spec"
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		return fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod")), nil
	}
	fakeExportSystem := &FakeExportSystem{}
	reconciler := Reconciler{
		reader:              directK8sClient,
		writer:              directK8sClient,
		scheme:              mgr.GetScheme(),
		system:              fakeExportSystem,
		auditConnectionName: connectionName,
		getPod:              getPod,
	}
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: connectionName, Namespace: util.GetNamespace()}}

	noSpec := &unstructured.Unstructured{}
	noSpec.SetGroupVersionKind(connectionv1alpha1.GroupVersion.WithKind("Connection"))
	noSpec.SetName(connectionName)
	noSpec.SetNamespace(util.GetNamespace())
	require.NoError(t, directK8sClient.Create(ctx, noSpec))

	pod, _ := getPod(ctx)
	statusName, err := statusv1alpha1.KeyForConnection(pod.Name, util.GetNamespace(), connectionName)
	require.NoError(t, err)
	statusKey := types.NamespacedName{Name: statusName, Namespace: util.GetNamespace()}
	connPodStatus := &statusv1alpha1.ConnectionPodStatus{}
	t.Cleanup(func() {
		_ = directK8sClient.Delete(context.Background(), noSpec)
		_ = directK8sClient.Delete(context.Background(), connPodStatus)
	})

	result, err := reconciler.Reconcile(ctx, request)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	g.Expect(result.Requeue).Should(gomega.BeTrue()) // nolint:staticcheck
	g.Expect(fakeExportSystem.UpsertConnectionCalledCount).Should(gomega.Equal(0), "the export system must not see an incomplete Connection")
	g.Expect(directK8sClient.Get(ctx, statusKey, connPodStatus)).Should(gomega.Succeed())
	g.Expect(connPodStatus.Status.ConnectionErrors).Should(gomega.HaveLen(1))
	g.Expect(connPodStatus.Status.ConnectionErrors[0].Type).Should(gomega.Equal(statusv1alpha1.UpsertConnectionError))
	g.Expect(connPodStatus.Status.ConnectionErrors[0].Message).Should(gomega.Equal("spec.config is required"))

	connObj := &connectionv1alpha1.Connection{}
	require.NoError(t, directK8sClient.Get(ctx, request.NamespacedName, connObj))
	connObj.Spec = connectionv1alpha1.ConnectionSpec{
		Driver: disk.Name,
		Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
	}
	require.NoError(t, directK8sClient.Update(ctx, connObj))

	result, err = reconciler.Reconcile(ctx, request)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	g.Expect(result.Requeue).Should(gomega.BeFalse()) // nolint:staticcheck
	g.Expect(fakeExportSystem.UpsertConnectionCalledCount).Should(gomega.Equal(1))
	g.Expect(directK8sClient.Get(ctx, statusKey, connPodStatus)).Should(gomega.Succeed())
	g.Expect(connPodStatus.Status.ConnectionErrors).Should(gomega.BeEmpty(), "fixing the Connection clears the error without a restart")
}

// Mock K8s client to simulate the client failures and impact on the controller.
func TestReconcile_Client_Failures(t *testing.T) {
	// Setup
	auditConnectionName := "audit-connection-3"
	auditConnectionNameFlag := fmt.Sprintf("--audit-connection=%s", auditConnectionName)
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", auditConnectionNameFlag}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}

	t.Run("Test GET returns error causes requeue", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionName,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      auditConnectionName,
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
			reader:              fakeClient,
			writer:              fakeClient,
			scheme:              mgr.GetScheme(),
			system:              fakeExportSystem,
			auditConnectionName: auditConnectionName,
			getPod:              getPod,
		}

		// Test setup Create the Connection object
		g.Expect(directK8sClient.Create(ctx, &connObj)).Should(gomega.Succeed())

		// Call Reconcile directly to assert the behavior on failures without having controller go through requeues
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeConnectionNamespacedName})
		g.Expect(result.Requeue).Should(gomega.Equal(false), "Reconcile should not requeue after the GET error") // nolint:staticcheck
		g.Expect(err).Should(gomega.Equal(mockErr), "Reconcile should return an error")

		// Cleanup the Connection object if it exists at the end
		defer func() {
			directK8sClient.Delete(ctx, &connObj) // nolint:errcheck
		}()
	})
}

func TestReconcile_ConnectionPodStatus(t *testing.T) {
	// Setup
	auditConnectionName := "audit-connection-4"
	auditConnectionNameFlag := fmt.Sprintf("--audit-connection=%s", auditConnectionName)
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", auditConnectionNameFlag}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		pod := fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod"))
		return pod, nil
	}
	// Required for the test PodToConnectionMapper to pickup the test pod name
	os.Setenv("POD_NAME", "no-pod")

	// Wrap the controller Reconciler so it writes each request to a map when it is finished reconciling
	originalReconciler := newReconciler(mgr, export.NewSystem(), auditConnectionName, getPod)
	wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
	// Register the controller with the manager
	require.NoError(t, add(mgr, wrappedReconciler, originalReconciler.auditConnectionName))
	// Start the manager and let it run in the background
	testutils.StartManager(ctx, t, mgr)

	t.Run("Reconcile ignores publish-only status updates", func(t *testing.T) {
		connObj := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionName,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      auditConnectionName,
			Namespace: util.GetNamespace(),
		}

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

		// Assert the ConnectionPodStatus
		connPodStatusObj := statusv1alpha1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusName, _ := statusv1alpha1.KeyForConnection(pod.Name, connObj.Namespace, connObj.Name)
		typeStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusName,
			Namespace: util.GetNamespace(),
		}
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObj.Status.PublishStatuses).Should(gomega.BeEmpty(), "No publish operations have been performed yet")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Update on the side to force the reconcile to be called
		connPodStatusObj.Status.ConnectionErrors = []*statusv1alpha1.ConnectionError{
			{
				Type:    statusv1alpha1.UpsertConnectionError,
				Message: "Mock error for testing",
			},
		}

		// Clear the previous request with the same name to avoid false positives now only load the latest
		requests.Clear()

		g.Expect(k8sClient.Update(ctx, &connPodStatusObj)).Should(gomega.Succeed(), "Updating the connection pod status should succeed")

		// Await for the reconcile request to finish
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Reconcile request should finish after updating the connection pod status")

		// Assert the ConnectionPodStatus
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should still exist after updating the connection pod status")
			g.Expect(connPodStatusObj.Status.ConnectionErrors).Should(gomega.BeEmpty(), "A successful upsert should clear connection errors")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
		g.Eventually(func() bool {
			requests.Clear()
			time.Sleep(50 * time.Millisecond)
			_, finished := requests.Load(expectedReq)
			return !finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Previous status reconciliation should become idle")
		requests.Clear()
		g.Eventually(func() error {
			latest := &statusv1alpha1.ConnectionPodStatus{}
			if err := k8sClient.Get(ctx, typeStatusNamespacedName, latest); err != nil {
				return err
			}
			latest.Status.PublishStatuses = []statusv1alpha1.ConnectionPublishStatus{
				{
					Source: statusv1alpha1.AuditPublishSource,
					Errors: []*statusv1alpha1.ConnectionError{
						{Type: statusv1alpha1.PublishError, Message: "backend unavailable"},
					},
				},
			}
			return k8sClient.Update(ctx, latest)
		}).WithTimeout(timeout).Should(gomega.Succeed(), "Updating publish status should succeed")
		g.Consistently(func() bool {
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(time.Second).Should(gomega.BeFalse(), "Publish-only status should not trigger export reconciliation")
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObj)
			g.Expect(err).Should(gomega.Succeed(), "Status should still exist after updating publish status")
			g.Expect(connPodStatusObj.Status.PublishStatuses).Should(gomega.Equal([]statusv1alpha1.ConnectionPublishStatus{
				{
					Source: statusv1alpha1.AuditPublishSource,
					Errors: []*statusv1alpha1.ConnectionError{
						{Type: statusv1alpha1.PublishError, Message: "backend unavailable"},
					},
				},
			}))
		}).WithTimeout(timeout).Should(gomega.Succeed())

		// Cleanup the Connection and ConnectionPodStatus object if it exists at the end
		defer func() {
			k8sClient.Delete(ctx, &connPodStatusObj) // nolint:errcheck
			k8sClient.Delete(ctx, &connObj)          // nolint:errcheck
		}()
	})
}

func TestReconcile_UnsupportedConnectionName(t *testing.T) {
	auditConnectionNameGood := "audit-connection-good"
	auditConnectionNameFlag := fmt.Sprintf("--audit-connection=%s", auditConnectionNameGood)
	require.NoError(t, flag.CommandLine.Parse([]string{"--enable-violation-export=true", auditConnectionNameFlag}), "parsing flags")

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	g := gomega.NewGomegaWithT(t)
	mgr, _ := testutils.SetupManager(t, cfg)
	k8sClient := testclient.NewRetryClient(mgr.GetClient())
	getPod := func(_ context.Context) (*corev1.Pod, error) {
		return fakes.Pod(fakes.WithNamespace("gatekeeper-system"), fakes.WithName("no-pod")), nil
	}
	originalReconciler := newReconciler(mgr, export.NewSystem(), auditConnectionNameGood, getPod)
	wrappedReconciler, requests := testutils.SetupTestReconcile(originalReconciler)
	require.NoError(t, add(mgr, wrappedReconciler, originalReconciler.auditConnectionName))
	testutils.StartManager(ctx, t, mgr)

	t.Run("Reconcile called for new Connection create for an unsupported connection name and the ConnectionPodStatus has an UpsertError and doesn't impact Create for a valid Connection object", func(t *testing.T) {
		auditConnectionNameBad := "audit-connection-bad"
		connObjBad := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionNameBad,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedName := types.NamespacedName{
			Name:      auditConnectionNameBad,
			Namespace: util.GetNamespace(),
		}

		g.Expect(k8sClient.Get(ctx, typeConnectionNamespacedName, &connObjBad)).ShouldNot(gomega.Succeed(), "Resource should not exist before creation")
		g.Expect(k8sClient.Create(ctx, &connObjBad)).Should(gomega.Succeed())
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		connPodStatusObjBad := statusv1alpha1.ConnectionPodStatus{}
		pod, _ := getPod(ctx)
		connPodStatusNameBad, _ := statusv1alpha1.KeyForConnection(pod.Name, connObjBad.Namespace, connObjBad.Name)
		typeStatusNamespacedNameBad := types.NamespacedName{
			Name:      connPodStatusNameBad,
			Namespace: util.GetNamespace(),
		}
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedNameBad, &connPodStatusObjBad)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObjBad.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObjBad.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObjBad.Status.ConnectionErrors).ShouldNot(gomega.BeEmpty(), "Status should have an error after creation for unsupported connection name")
			g.Expect(connPodStatusObjBad.Status.ConnectionErrors[0].Message).Should(gomega.ContainSubstring("unsupported"), "Status should have an error with expected message for unsupported connection name")
			g.Expect(connPodStatusObjBad.Status.ConnectionErrors[0].Type).Should(gomega.Equal(statusv1alpha1.UpsertConnectionError), "Status should have an error with expected type for unsupported connection name")
			g.Expect(connPodStatusObjBad.Status.ObservedGeneration).Should(gomega.Equal(connObjBad.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObjBad.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObjBad.Status.ConnectionUID).Should(gomega.Equal(connObjBad.GetUID()), "ConnectionPodStatus UID should match the connection object UID")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		g.Expect(k8sClient.Delete(ctx, &connObjBad)).Should(gomega.Succeed(), "Deleting the connection object should succeed")
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedName}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Reconcile request should finish after deleting the connection object")
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedNameBad, &connObjBad)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection obj cleaned up after deleting the connection object")
			err = k8sClient.Get(ctx, typeStatusNamespacedNameBad, &connPodStatusObjBad)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection pod status should get cleaned up after deleting the connection object")
		}).WithTimeout(timeout)

		requests.Clear()
		connObjGood := connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      auditConnectionNameGood,
				Namespace: util.GetNamespace(),
			},
			Spec: connectionv1alpha1.ConnectionSpec{
				Driver: disk.Name,
				Config: &anythingtypes.Anything{Value: diskConnectionConfig(t.TempDir())},
			},
		}
		typeConnectionNamespacedNameGood := types.NamespacedName{
			Name:      auditConnectionNameGood,
			Namespace: util.GetNamespace(),
		}

		g.Expect(k8sClient.Get(ctx, typeConnectionNamespacedNameGood, &connObjGood)).ShouldNot(gomega.Succeed(), "Resource should not exist before creation")
		g.Expect(k8sClient.Create(ctx, &connObjGood)).Should(gomega.Succeed())
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedNameGood}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue())

		connPodStatusObjGood := statusv1alpha1.ConnectionPodStatus{}
		connPodStatusNameGood, _ := statusv1alpha1.KeyForConnection(pod.Name, connObjGood.Namespace, connObjGood.Name)
		typeStatusNamespacedName := types.NamespacedName{
			Name:      connPodStatusNameGood,
			Namespace: util.GetNamespace(),
		}
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObjGood)
			g.Expect(err).Should(gomega.Succeed(), "Status should exist after creation")
			g.Expect(connPodStatusObjGood.GetLabels()).Should(gomega.HaveKeyWithValue(statusv1beta1.ConnectionNameLabel, connObjGood.Name), "Status should have the correct connection name label")
			g.Expect(connPodStatusObjGood.Status.ConnectionErrors).Should(gomega.BeEmpty(), "Status should not have an error after creation for supported connection name")
			g.Expect(connPodStatusObjGood.Status.ObservedGeneration).Should(gomega.Equal(connObjGood.GetGeneration()), "Observed generation should match the connection object generation")
			g.Expect(connPodStatusObjGood.Status.ID).Should(gomega.Equal(pod.Name), "ID should match the pod name")
			g.Expect(connPodStatusObjGood.Status.ConnectionUID).Should(gomega.Equal(connObjGood.GetUID()), "ConnectionPodStatus UID should match the Connection object UID")
		}).WithTimeout(timeout).Should(gomega.Succeed())

		g.Expect(k8sClient.Delete(ctx, &connObjGood)).Should(gomega.Succeed(), "Deleting the connection object should succeed")
		g.Eventually(func() bool {
			expectedReq := reconcile.Request{NamespacedName: typeConnectionNamespacedNameGood}
			_, finished := requests.Load(expectedReq)
			return finished
		}).WithTimeout(timeout).Should(gomega.BeTrue(), "Reconcile request should finish after deleting the connection object")
		g.Eventually(func(g gomega.Gomega) {
			err := k8sClient.Get(ctx, typeStatusNamespacedName, &connObjGood)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection obj cleaned up after deleting the connection object")
			err = k8sClient.Get(ctx, typeStatusNamespacedName, &connPodStatusObjGood)
			g.Expect(err).ShouldNot(gomega.Succeed(), "Connection pod status should get cleaned up after deleting the connection object")
		}).WithTimeout(timeout)

		defer func() {
			k8sClient.Delete(ctx, &connObjBad)           // nolint:errcheck
			k8sClient.Delete(ctx, &connObjGood)          // nolint:errcheck
			k8sClient.Delete(ctx, &connPodStatusObjBad)  // nolint:errcheck
			k8sClient.Delete(ctx, &connPodStatusObjGood) // nolint:errcheck
		}()
	})
}

func TestReconcileConnectionState(t *testing.T) {
	testutils.Setenv(t, "POD_NAMESPACE", "gatekeeper-system")
	const auditConnectionName = "audit-connection"
	testScheme := runtime.NewScheme()
	require.NoError(t, connectionv1alpha1.AddToScheme(testScheme))
	require.NoError(t, statusv1alpha1.AddToScheme(testScheme))
	require.NoError(t, corev1.AddToScheme(testScheme))
	pod := fakes.Pod(fakes.WithNamespace(util.GetNamespace()), fakes.WithName("gatekeeper-pod"))
	connection := func(name string, withConfig bool) *connectionv1alpha1.Connection {
		conn := &connectionv1alpha1.Connection{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: util.GetNamespace()}}
		if withConfig {
			conn.Spec = connectionv1alpha1.ConnectionSpec{Driver: disk.Name, Config: &anythingtypes.Anything{Value: diskConnectionConfig("/tmp/violations")}}
		}
		return conn
	}
	valid := connection(auditConnectionName, true)
	mockErr := errors.New("mock error")

	type step struct {
		// connection is the object present for the reconcile; nil means it does not exist.
		connection  *connectionv1alpha1.Connection
		getErr      error
		upsertErr   error
		closeErr    error
		getPodErr   error
		wantState   connectionState
		wantRequeue bool
		wantErr     bool
	}
	tests := []struct {
		name            string
		steps           []step
		wantUpserts     int
		wantCloses      int
		wantStatuses    int
		wantRemovedLogs int
	}{
		{
			name:       "missing connection is not configured",
			steps:      []step{{wantState: stateNotConfigured}},
			wantCloses: 1,
		},
		{
			name:       "missing connection with failing pod lookup",
			steps:      []step{{getPodErr: mockErr, wantState: stateNotConfigured, wantErr: true}},
			wantCloses: 1,
		},
		{
			name: "failed close is not configured and requeues",
			steps: []step{
				{connection: valid, wantState: stateReady},
				{closeErr: mockErr, wantState: stateNotConfigured, wantRequeue: true},
			},
			wantUpserts: 1,
			wantCloses:  1,
		},
		{
			name: "deleting a ready connection logs removal once",
			steps: []step{
				{connection: valid, wantState: stateReady},
				{wantState: stateNotConfigured},
				{wantState: stateNotConfigured},
			},
			wantUpserts:     1,
			wantCloses:      2,
			wantRemovedLogs: 1,
		},
		{
			name: "get error leaves state unchanged",
			steps: []step{
				{connection: valid, wantState: stateReady},
				{connection: valid, getErr: mockErr, wantState: stateReady, wantErr: true},
			},
			wantUpserts:  1,
			wantStatuses: 1,
		},
		{
			name: "invalid edit keeps the working connection",
			steps: []step{
				{connection: valid, wantState: stateReady},
				{connection: connection(auditConnectionName, false), wantState: stateError, wantRequeue: true},
			},
			wantUpserts:  1,
			wantStatuses: 1,
		},
		{
			name: "upsert failure recovers without an edit",
			steps: []step{
				{connection: valid, upsertErr: mockErr, wantState: stateError, wantRequeue: true},
				{connection: valid, wantState: stateReady},
			},
			wantUpserts:  2,
			wantStatuses: 1,
		},
		{
			name:         "unsupported connection name leaves state unchanged",
			steps:        []step{{connection: connection("other-connection", true), wantState: stateUnknown}},
			wantStatuses: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			k8sClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
			reader := &FakeClient{Client: k8sClient}
			system := &FakeExportSystem{}
			var getPodErr error
			var logs []string
			r := &Reconciler{
				reader:              reader,
				writer:              k8sClient,
				scheme:              testScheme,
				system:              system,
				auditConnectionName: auditConnectionName,
				getPod: func(context.Context) (*corev1.Pod, error) {
					return pod, getPodErr
				},
				log: funcr.New(func(_, args string) { logs = append(logs, args) }, funcr.Options{}),
			}

			for i, st := range tc.steps {
				name := auditConnectionName
				existing := &connectionv1alpha1.Connection{}
				existingErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: util.GetNamespace(), Name: name}, existing)
				switch {
				case st.connection == nil && existingErr == nil:
					require.NoError(t, k8sClient.Delete(ctx, existing))
				case st.connection != nil:
					name = st.connection.Name
					desired := st.connection.DeepCopy()
					if existingErr == nil && existing.Name == name {
						desired.ResourceVersion = existing.ResourceVersion
						require.NoError(t, k8sClient.Update(ctx, desired))
					} else {
						require.NoError(t, k8sClient.Create(ctx, desired))
					}
				}
				reader.getErr = st.getErr
				system.UpsertConnectionError = st.upsertErr
				system.CloseConnectionError = st.closeErr
				getPodErr = st.getPodErr

				result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: util.GetNamespace(), Name: name}})
				require.Equal(t, st.wantErr, err != nil, "step %d: %v", i, err)
				require.Equal(t, st.wantRequeue, result.Requeue, "step %d", i) // nolint:staticcheck
				require.Equal(t, st.wantState, connectionState(r.state.Load()), "step %d", i)
			}

			require.Equal(t, tc.wantUpserts, system.UpsertConnectionCalledCount)
			require.Equal(t, tc.wantCloses, system.CloseConnectionCalledCount)
			statuses := &statusv1alpha1.ConnectionPodStatusList{}
			require.NoError(t, k8sClient.List(ctx, statuses))
			require.Len(t, statuses.Items, tc.wantStatuses)
			removedLogs := 0
			for _, entry := range logs {
				if strings.Contains(entry, `"msg"="removed connection"`) {
					removedLogs++
				}
			}
			require.Equal(t, tc.wantRemovedLogs, removedLogs)
		})
	}
}

func TestSeedConnectionSourceQueuesConfiguredConnection(t *testing.T) {
	testutils.Setenv(t, "POD_NAMESPACE", "gatekeeper-system")
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()

	require.NoError(t, seedConnectionSource("audit-connection").Start(context.Background(), queue))

	require.Equal(t, 1, queue.Len())
	request, _ := queue.Get()
	require.Equal(t, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "gatekeeper-system", Name: "audit-connection"}}, request)
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

func (f *FakeExportSystem) Publish(_ context.Context, _ string, _ string, _ interface{}) error {
	f.PublishCalledCount++
	if f.PublishError != nil {
		return f.PublishError
	}
	return nil
}

func (f *FakeExportSystem) UpsertConnection(_ context.Context, _ interface{}, _ string, _ string) error {
	f.UpsertConnectionCalledCount++
	if f.UpsertConnectionError != nil {
		return f.UpsertConnectionError
	}
	return nil
}

func (f *FakeExportSystem) CloseConnection(_ string) error {
	f.CloseConnectionCalledCount++
	if f.CloseConnectionError != nil {
		return f.CloseConnectionError
	}
	return nil
}
