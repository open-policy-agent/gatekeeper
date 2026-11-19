package audit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestAuditTriggerReconcile(t *testing.T) {
	for _, tt := range []struct {
		name       string
		successful int64
		failed     int64
		wantWake   bool
	}{
		{name: "new request", wantWake: true},
		{name: "success preserved", successful: 7},
		{name: "failure requires new request", failed: 7},
		{name: "newer request after failure", failed: 6, wantWake: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trigger := newAuditTrigger(7)
			trigger.Status.SuccessfulGeneration = tt.successful
			trigger.Status.FailedGeneration = tt.failed
			c := newAuditTriggerClient(t, trigger)
			wake := make(chan struct{}, 1)
			r := &auditTriggerReconciler{client: c, wake: wake}
			for range 2 {
				_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: auditTriggerKey})
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantWake, len(wake) == 1)
			got := getAuditTrigger(t, c)
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
	wake := make(chan struct{}, 1)
	r := &auditTriggerReconciler{client: c, wake: wake}
	_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(trigger)})
	require.NoError(t, err)
	require.Empty(t, wake)
}

func TestAuditTriggerRunCoalescesAndPreservesCompletion(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	runs := 0
	resets := 0
	runner := newAuditTriggerRunner(c, func(context.Context) error {
		runs++
		require.Equal(t, runs-1, resets)
		if runs == 1 {
			running := getAuditTrigger(t, c).Status.LastRun
			require.Equal(t, "Running", running.Phase)
			require.Nil(t, running.CompletedAt)
			require.EqualValues(t, 7, running.CoveredGeneration)
			advanceAuditTrigger(t, c, 12)
			reconciler := &auditTriggerReconciler{client: c, wake: make(chan struct{}, 1)}
			_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: auditTriggerKey})
			require.NoError(t, err)
		}
		return nil
	})
	runner.resetTicker = func() {
		status := getAuditTrigger(t, c).Status
		require.Equal(t, "Succeeded", status.LastRun.Phase)
		require.Equal(t, status.LastRun.CoveredGeneration, status.SuccessfulGeneration)
		resets++
	}
	require.NoError(t, runner.run(context.Background(), true))
	got := getAuditTrigger(t, c)
	require.EqualValues(t, 7, got.Status.SuccessfulGeneration)
	require.EqualValues(t, 7, got.Status.LastRun.CoveredGeneration)
	require.EqualValues(t, 12, got.Status.AcknowledgedGeneration)

	require.NoError(t, runner.run(context.Background(), true))
	require.Equal(t, 2, runs)
	got = getAuditTrigger(t, c)
	require.EqualValues(t, 12, got.Status.SuccessfulGeneration)
	require.NotNil(t, got.Status.LastRun.CompletedAt)
	require.NoError(t, runner.run(context.Background(), true))
	require.Equal(t, 2, runs)
	require.Equal(t, 2, resets)
}

func TestAuditTriggerFailureOrdering(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(map[bool]string{true: "failure then success", false: "success then failure"}[failFirst], func(t *testing.T) {
			c := newAuditTriggerClient(t, newAuditTrigger(10))
			auditErr := errors.New("list failed")
			runs := 0
			resets := 0
			runner := newAuditTriggerRunner(c, func(context.Context) error {
				runs++
				if (runs == 1) == failFirst {
					return auditErr
				}
				return nil
			})
			runner.resetTicker = func() { resets++ }
			err := runner.run(context.Background(), true)
			if failFirst {
				require.ErrorIs(t, err, auditErr)
				require.Zero(t, resets)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, resets)
			}
			advanceAuditTrigger(t, c, 11)
			err = runner.run(context.Background(), true)
			if failFirst {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, auditErr)
			}
			got := getAuditTrigger(t, c)
			if failFirst {
				require.EqualValues(t, 11, got.Status.SuccessfulGeneration)
				require.EqualValues(t, 10, got.Status.FailedGeneration)
			} else {
				require.EqualValues(t, 10, got.Status.SuccessfulGeneration)
				require.EqualValues(t, 11, got.Status.FailedGeneration)
			}
			require.NoError(t, runner.run(context.Background(), true))
			require.Equal(t, 2, runs)
			require.Equal(t, 1, resets)
		})
	}
}

func TestAuditTriggerPeriodicRunAndRecovery(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	ctx, cancel := context.WithCancel(context.Background())
	resets := 0
	runner := newAuditTriggerRunner(c, func(context.Context) error {
		cancel()
		return nil
	})
	runner.resetTicker = func() { resets++ }
	require.ErrorIs(t, runner.run(ctx, true), context.Canceled)
	require.Zero(t, resets)
	got := getAuditTrigger(t, c)
	require.Zero(t, got.Status.SuccessfulGeneration)
	require.Zero(t, got.Status.FailedGeneration)
	require.Equal(t, "Running", got.Status.LastRun.Phase)

	runs := 0
	recovered := newAuditTriggerRunner(c, func(context.Context) error {
		runs++
		return nil
	})
	recovered.resetTicker = func() { resets++ }
	require.NoError(t, recovered.run(context.Background(), true))
	require.EqualValues(t, 7, getAuditTrigger(t, c).Status.SuccessfulGeneration)
	require.NoError(t, recovered.run(context.Background(), true))
	require.Equal(t, 1, runs)
	require.NoError(t, recovered.run(context.Background(), false))
	require.Equal(t, 2, runs)
	require.Equal(t, 1, resets)
}

func TestAuditTriggerDoesNotCreditRecreatedObject(t *testing.T) {
	trigger := newAuditTrigger(7)
	c := newAuditTriggerClient(t, trigger)
	runner := newAuditTriggerRunner(c, func(ctx context.Context) error {
		require.NoError(t, c.Delete(ctx, getAuditTrigger(t, c)))
		replacement := newAuditTrigger(1)
		replacement.UID = "replacement"
		require.NoError(t, c.Create(ctx, replacement))
		return nil
	})
	require.NoError(t, runner.run(context.Background(), true))
	require.Zero(t, getAuditTrigger(t, c).Status.SuccessfulGeneration)
}

func TestAuditTriggerRetriesCompletionWrite(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	fail := true
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			trigger, ok := obj.(*auditv1alpha1.AuditTrigger)
			if ok && trigger.Status.SuccessfulGeneration == 7 && fail {
				fail = false
				return apierrors.NewConflict(schema.GroupResource{Group: "audit.gatekeeper.sh", Resource: "audittriggers"}, trigger.Name, errors.New("conflict"))
			}
			return c.SubResource(name).Update(ctx, obj, opts...)
		},
	})
	runs := 0
	runner := newAuditTriggerRunner(wrapped, func(context.Context) error {
		runs++
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, runner.run(ctx, true))
	require.False(t, fail)
	require.Equal(t, 1, runs)
	require.EqualValues(t, 7, getAuditTrigger(t, c).Status.SuccessfulGeneration)
}

func TestAuditTriggerPeriodicRunWithoutSingleton(t *testing.T) {
	c := newAuditTriggerClient(t)
	runs := 0
	runner := newAuditTriggerRunner(c, func(context.Context) error {
		runs++
		return nil
	})
	require.NoError(t, runner.run(context.Background(), true))
	require.Zero(t, runs)
	require.NoError(t, runner.run(context.Background(), false))
	require.Equal(t, 1, runs)
}

func TestAuditTriggerStatusFailureDoesNotBlockPeriodicScans(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	reject := true
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if reject {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers/status"}, obj.GetName(), errors.New("denied"))
			}
			return c.SubResource(name).Update(ctx, obj, opts...)
		},
	})
	runs := 0
	runner := newAuditTriggerRunner(wrapped, func(context.Context) error {
		runs++
		if runs == 2 {
			return errors.New("later failure")
		}
		return nil
	})
	require.Error(t, runner.run(context.Background(), false))
	require.Equal(t, 1, runs)
	advanceAuditTrigger(t, c, 8)
	require.Error(t, runner.run(context.Background(), false))
	require.Equal(t, 2, runs)
	reject = false
	require.NoError(t, runner.run(context.Background(), true))
	require.Equal(t, 2, runs)
	got := getAuditTrigger(t, c).Status
	require.EqualValues(t, 7, got.SuccessfulGeneration)
	require.EqualValues(t, 8, got.FailedGeneration)
	require.Nil(t, runner.pending)
}

func TestAuditTriggerCompletionAfterStartWriteFailure(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if trigger, ok := obj.(*auditv1alpha1.AuditTrigger); ok && trigger.Status.LastRun.Phase == "Running" {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers/status"}, obj.GetName(), errors.New("denied"))
			}
			return c.SubResource(name).Update(ctx, obj, opts...)
		},
	})
	runner := newAuditTriggerRunner(wrapped, func(context.Context) error { return nil })
	require.NoError(t, runner.run(context.Background(), false))
	got := getAuditTrigger(t, c).Status
	require.EqualValues(t, 7, got.SuccessfulGeneration)
	require.Equal(t, "Succeeded", got.LastRun.Phase)
	require.NotNil(t, got.LastRun.CompletedAt)
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

func TestPeriodicAuditFailureDoesNotScheduleRetry(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	var reads atomic.Int32
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads.Add(1)
			return c.Get(ctx, key, obj, opts...)
		},
	})
	runner := newAuditTriggerRunner(wrapped, func(context.Context) error { return errors.New("audit failed") })
	am := &Manager{stopper: make(chan struct{}), auditTriggerWake: make(chan struct{}, 1)}
	ticks := make(chan time.Time, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go am.runAuditLoop(ctx, ticks, runner)
	t.Cleanup(func() {
		cancel()
		select {
		case <-am.stopper:
		case <-time.After(5 * time.Second):
			t.Error("audit loop did not stop")
		}
	})
	ticks <- time.Now()
	require.Eventually(t, func() bool {
		trigger := getAuditTrigger(t, c)
		return trigger.Status.FailedGeneration == 7
	}, 5*time.Second, 10*time.Millisecond)
	count := reads.Load()
	require.Never(t, func() bool { return reads.Load() != count }, 1500*time.Millisecond, 10*time.Millisecond)
}

func newAuditTriggerRunner(c client.Client, audit func(context.Context) error) *auditTriggerRunner {
	return &auditTriggerRunner{reader: c, client: c, audit: audit, now: time.Now}
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

func getAuditTrigger(t *testing.T, c client.Client) *auditv1alpha1.AuditTrigger {
	t.Helper()
	trigger := &auditv1alpha1.AuditTrigger{}
	require.NoError(t, c.Get(context.Background(), auditTriggerKey, trigger))
	return trigger
}

func advanceAuditTrigger(t *testing.T, c client.Client, generation int64) {
	t.Helper()
	trigger := getAuditTrigger(t, c)
	trigger.Spec.AuditNonce = "next-nonce"
	trigger.Generation = generation
	require.NoError(t, c.Update(context.Background(), trigger))
}
