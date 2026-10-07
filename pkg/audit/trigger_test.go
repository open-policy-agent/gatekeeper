package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestAuditTriggerReconcile(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		trigger       *auditv1alpha1.AuditTrigger
		wantDelay     time.Duration
		wantRequest   bool
		wantCondition bool
	}{
		{
			name: "waits until requested timestamp",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("future", 1, now.Add(time.Minute)),
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			),
			wantDelay: time.Minute,
		},
		{
			name:          "acknowledges a new generation",
			trigger:       newAuditTrigger("new", 1, now),
			wantCondition: true,
		},
		{
			name: "queues an acknowledged generation",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("acknowledged", 2, now),
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			),
			wantRequest: true,
		},
		{
			name: "does not queue a completed generation",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("completed", 3, now),
				metav1.ConditionTrue,
				metav1.ConditionTrue,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, auditv1alpha1.AddToScheme(scheme))
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&auditv1alpha1.AuditTrigger{}).
				WithObjects(tt.trigger).
				Build()
			requests := make(chan auditRequest, 1)
			pending := &sync.Map{}
			results := &sync.Map{}
			r := &auditTriggerReconciler{
				client:   k8sClient,
				requests: requests,
				pending:  pending,
				results:  results,
				now:      func() time.Time { return now },
			}

			result, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(tt.trigger),
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantDelay, result.RequeueAfter)

			select {
			case got := <-requests:
				require.True(t, tt.wantRequest)
				require.Equal(t, tt.trigger.Generation, got.generation)
				require.Equal(t, tt.trigger.UID, got.uid)
			default:
				require.False(t, tt.wantRequest)
			}

			if tt.wantCondition {
				got := &auditv1alpha1.AuditTrigger{}
				require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tt.trigger), got))
				acknowledged := meta.FindStatusCondition(got.Status.Conditions, auditv1alpha1.ConditionAcknowledged)
				require.NotNil(t, acknowledged)
				require.Equal(t, metav1.ConditionTrue, acknowledged.Status)
				require.Equal(t, got.Generation, acknowledged.ObservedGeneration)
				succeeded := meta.FindStatusCondition(got.Status.Conditions, auditv1alpha1.ConditionSucceeded)
				require.NotNil(t, succeeded)
				require.Equal(t, metav1.ConditionUnknown, succeeded.Status)
				require.Equal(t, got.Generation, succeeded.ObservedGeneration)
			}
		})
	}
}

func TestAuditTriggerReconcileQueuesGenerationOnce(t *testing.T) {
	now := time.Now()
	trigger := auditTriggerWithConditions(
		newAuditTrigger("once", 1, now),
		metav1.ConditionTrue,
		metav1.ConditionUnknown,
	)
	scheme := runtime.NewScheme()
	require.NoError(t, auditv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&auditv1alpha1.AuditTrigger{}).
		WithObjects(trigger).
		Build()
	requests := make(chan auditRequest, 2)
	r := &auditTriggerReconciler{
		client:   k8sClient,
		requests: requests,
		pending:  &sync.Map{},
		results:  &sync.Map{},
		now:      func() time.Time { return now },
	}
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(trigger)}

	_, err := r.Reconcile(context.Background(), request)
	require.NoError(t, err)
	_, err = r.Reconcile(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, requests, 1)
}

func TestAuditTriggerReconcileRetriesWhenQueueIsFull(t *testing.T) {
	now := time.Now()
	trigger := auditTriggerWithConditions(
		newAuditTrigger("retry", 1, now),
		metav1.ConditionTrue,
		metav1.ConditionUnknown,
	)
	scheme := runtime.NewScheme()
	require.NoError(t, auditv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&auditv1alpha1.AuditTrigger{}).
		WithObjects(trigger).
		Build()
	requests := make(chan auditRequest, 1)
	requests <- auditRequest{}
	pending := &sync.Map{}
	r := &auditTriggerReconciler{
		client:   k8sClient,
		requests: requests,
		pending:  pending,
		results:  &sync.Map{},
		now:      func() time.Time { return now },
	}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKeyFromObject(trigger),
	})
	require.NoError(t, err)
	require.Equal(t, time.Second, result.RequeueAfter)
	_, exists := pending.Load(auditRequest{
		name:       client.ObjectKeyFromObject(trigger),
		uid:        trigger.UID,
		generation: trigger.Generation,
	}.key())
	require.False(t, exists)
}

func TestCompleteAuditTrigger(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		trigger    *auditv1alpha1.AuditTrigger
		requestGen int64
		auditErr   error
		wantStatus metav1.ConditionStatus
	}{
		{
			name: "records success",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("success", 1, now),
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			),
			requestGen: 1,
			wantStatus: metav1.ConditionTrue,
		},
		{
			name: "records failure",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("failure", 1, now),
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			),
			requestGen: 1,
			auditErr:   errors.New("audit failed"),
			wantStatus: metav1.ConditionFalse,
		},
		{
			name: "does not overwrite a newer generation",
			trigger: auditTriggerWithConditions(
				newAuditTrigger("newer", 2, now),
				metav1.ConditionTrue,
				metav1.ConditionUnknown,
			),
			requestGen: 1,
			wantStatus: metav1.ConditionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, auditv1alpha1.AddToScheme(scheme))
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&auditv1alpha1.AuditTrigger{}).
				WithObjects(tt.trigger).
				Build()
			requests := make(chan auditRequest, 1)
			request := auditRequest{
				name:       client.ObjectKeyFromObject(tt.trigger),
				uid:        tt.trigger.UID,
				generation: tt.requestGen,
			}
			pending := &sync.Map{}
			pending.Store(request.key(), struct{}{})
			results := &sync.Map{}
			results.Store(request.key(), auditResult{err: tt.auditErr})
			r := &auditTriggerReconciler{
				client:   k8sClient,
				requests: requests,
				pending:  pending,
				results:  results,
				now:      func() time.Time { return now },
			}

			_, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(tt.trigger),
			})
			require.NoError(t, err)
			_, exists := pending.Load(request.key())
			require.False(t, exists)
			_, exists = results.Load(request.key())
			require.False(t, exists)

			got := &auditv1alpha1.AuditTrigger{}
			require.NoError(t, k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tt.trigger), got))
			succeeded := meta.FindStatusCondition(got.Status.Conditions, auditv1alpha1.ConditionSucceeded)
			require.NotNil(t, succeeded)
			require.Equal(t, tt.wantStatus, succeeded.Status)
		})
	}
}

func TestDrainAndReportAuditRequests(t *testing.T) {
	requests := []auditRequest{
		{name: types.NamespacedName{Name: "first"}, uid: "first", generation: 1},
		{name: types.NamespacedName{Name: "second"}, uid: "second", generation: 1},
		{name: types.NamespacedName{Name: "third"}, uid: "third", generation: 1},
	}
	am := &Manager{
		auditRequests:      make(chan auditRequest, len(requests)),
		auditTriggerEvents: make(chan event.GenericEvent, len(requests)),
	}
	am.auditRequests <- requests[1]
	am.auditRequests <- requests[2]

	got := am.drainAuditRequests(&requests[0])
	require.Equal(t, requests, got)
	require.Empty(t, am.auditRequests)

	auditErr := errors.New("audit failed")
	am.reportAuditResults(context.Background(), got, auditErr)
	require.Len(t, am.auditTriggerEvents, len(requests))
	for _, request := range requests {
		result, found := am.auditResults.Load(request.key())
		require.True(t, found)
		auditResult, ok := result.(auditResult)
		require.True(t, ok)
		require.ErrorIs(t, auditResult.err, auditErr)
	}
}

func TestNewAuditTicker(t *testing.T) {
	ticker, tickerC := newAuditTicker(0)
	require.Nil(t, ticker)
	require.Nil(t, tickerC)

	ticker, tickerC = newAuditTicker(time.Hour)
	require.NotNil(t, ticker)
	require.NotNil(t, tickerC)
	ticker.Stop()
}

func newAuditTrigger(name string, generation int64, timestamp time.Time) *auditv1alpha1.AuditTrigger {
	return &auditv1alpha1.AuditTrigger{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			UID:        types.UID(name + "-uid"),
			Generation: generation,
		},
		Spec: auditv1alpha1.AuditTriggerSpec{
			Timestamp: metav1.NewTime(timestamp),
		},
	}
}

func auditTriggerWithConditions(
	trigger *auditv1alpha1.AuditTrigger,
	acknowledged metav1.ConditionStatus,
	succeeded metav1.ConditionStatus,
) *auditv1alpha1.AuditTrigger {
	trigger.Status.Conditions = []metav1.Condition{
		{
			Type:               auditv1alpha1.ConditionAcknowledged,
			Status:             acknowledged,
			ObservedGeneration: trigger.Generation,
			Reason:             reasonTriggerAcknowledged,
		},
		{
			Type:               auditv1alpha1.ConditionSucceeded,
			Status:             succeeded,
			ObservedGeneration: trigger.Generation,
			Reason:             reasonAuditInProgress,
		},
	}
	return trigger
}
