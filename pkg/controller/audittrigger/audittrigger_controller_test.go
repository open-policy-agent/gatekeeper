package audittrigger

import (
	"context"
	"errors"
	"testing"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestAddToManagerRequiresNotify(t *testing.T) {
	require.ErrorContains(t, AddToManager(nil, nil), "notify callback must be non-nil")
}

func TestAuditTriggerReconcile(t *testing.T) {
	for _, tt := range []struct {
		name              string
		acknowledged      int64
		successful        int64
		failed            int64
		wantNotifications int
	}{
		{name: "new request", wantNotifications: 2},
		{name: "acknowledged request remains pending", acknowledged: 7, wantNotifications: 2},
		{name: "success preserved", successful: 7},
		{name: "failure requires new request", failed: 7},
		{name: "newer request after failure", failed: 6, wantNotifications: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trigger := newAuditTrigger(7)
			trigger.Status.AcknowledgedGeneration = tt.acknowledged
			trigger.Status.SuccessfulGeneration = tt.successful
			trigger.Status.FailedGeneration = tt.failed
			c := newAuditTriggerClient(t, trigger)
			notifications := 0
			r := &reconciler{client: c, notify: func() { notifications++ }}
			for range 2 {
				_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: triggerKey})
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantNotifications, notifications)
			got := &auditv1alpha1.AuditTrigger{}
			require.NoError(t, c.Get(context.Background(), triggerKey, got))
			require.EqualValues(t, 7, got.Status.AcknowledgedGeneration)
			require.Equal(t, tt.successful, got.Status.SuccessfulGeneration)
			require.Equal(t, tt.failed, got.Status.FailedGeneration)
		})
	}
}

func TestAuditTriggerIgnoresNonSingleton(t *testing.T) {
	trigger := newAuditTrigger(1)
	trigger.Name = "other"
	c := newAuditTriggerClient(t, trigger)
	notifications := 0
	r := &reconciler{client: c, notify: func() { notifications++ }}
	_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(trigger)})
	require.NoError(t, err)
	require.Zero(t, notifications)
}

func TestAuditTriggerStatusErrorDoesNotNotify(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	statusErr := errors.New("status update failed")
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
			return statusErr
		},
	})
	notifications := 0
	r := &reconciler{client: wrapped, notify: func() { notifications++ }}
	_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: triggerKey})
	require.ErrorIs(t, err, statusErr)
	require.Zero(t, notifications)
	got := &auditv1alpha1.AuditTrigger{}
	require.NoError(t, c.Get(context.Background(), triggerKey, got))
	require.Zero(t, got.Status.AcknowledgedGeneration)
}

func newAuditTriggerClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, auditv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&auditv1alpha1.AuditTrigger{}).WithObjects(objects...).Build()
}

func newAuditTrigger(generation int64) *auditv1alpha1.AuditTrigger {
	return &auditv1alpha1.AuditTrigger{
		ObjectMeta: metav1.ObjectMeta{Name: auditv1alpha1.AuditTriggerName, UID: "audit-trigger-uid", Generation: generation},
		Spec:       auditv1alpha1.AuditTriggerSpec{AuditNonce: "nonce"},
	}
}
