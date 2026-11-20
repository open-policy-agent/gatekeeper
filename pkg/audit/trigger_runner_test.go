package audit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestNotifyAuditTriggerCoalesces(t *testing.T) {
	am := &Manager{auditTriggerWake: make(chan struct{}, 1)}
	am.notifyAuditTrigger()
	done := make(chan struct{})
	go func() {
		am.notifyAuditTrigger()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		<-am.auditTriggerWake
		require.FailNow(t, "notification blocked on a full wake channel")
	}
	require.Len(t, am.auditTriggerWake, 1)
	<-am.auditTriggerWake
	am.notifyAuditTrigger()
	require.Len(t, am.auditTriggerWake, 1)
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
			trigger := getAuditTrigger(t, c)
			trigger.Status.AcknowledgedGeneration = 12
			require.NoError(t, c.Status().Update(context.Background(), trigger))
		}
		return nil
	})
	runner.resetTicker = func() {
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

func TestAuditTriggerStatusRetryBudget(t *testing.T) {
	permanentErr := apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers/status"}, "audit-trigger", errors.New("denied"))
	for _, tt := range []struct {
		name        string
		err         error
		cancel      bool
		wantErr     error
		minAttempts int
		maxAttempts int
	}{
		{name: "transient failure", err: apierrors.NewServiceUnavailable("unavailable"), wantErr: context.DeadlineExceeded, minAttempts: 2, maxAttempts: 5},
		{name: "permanent failure", err: permanentErr, wantErr: permanentErr, minAttempts: 1, maxAttempts: 1},
		{name: "cancellation", err: apierrors.NewServiceUnavailable("unavailable"), cancel: true, wantErr: context.Canceled, minAttempts: 1, maxAttempts: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trigger := newAuditTrigger(7)
			c := newAuditTriggerClient(t, trigger)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attempts := 0
			wrapped := interceptor.NewClient(c, interceptor.Funcs{
				SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
					attempts++
					if tt.cancel {
						cancel()
					}
					return tt.err
				},
			})
			runner := newAuditTriggerRunner(wrapped, func(context.Context) error { return nil })
			err := runner.updateStatus(ctx, trigger.UID, func(*auditv1alpha1.AuditTriggerStatus) {})
			require.ErrorIs(t, err, tt.wantErr)
			require.GreaterOrEqual(t, attempts, tt.minAttempts)
			require.LessOrEqual(t, attempts, tt.maxAttempts)
		})
	}
}

func TestAuditTriggerResetsScheduleDespiteCompletionWriteFailure(t *testing.T) {
	for _, onDemand := range []bool{false, true} {
		t.Run(map[bool]string{true: "on-demand", false: "scheduled"}[onDemand], func(t *testing.T) {
			c := newAuditTriggerClient(t, newAuditTrigger(7))
			reject := true
			wrapped := interceptor.NewClient(c, interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if trigger, ok := obj.(*auditv1alpha1.AuditTrigger); ok && trigger.Status.SuccessfulGeneration == 7 && reject {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers/status"}, obj.GetName(), errors.New("denied"))
					}
					return c.SubResource(name).Update(ctx, obj, opts...)
				},
			})
			runs, resets := 0, 0
			runner := newAuditTriggerRunner(wrapped, func(context.Context) error {
				runs++
				return nil
			})
			runner.resetTicker = func() { resets++ }
			wantResets := 0
			if onDemand {
				wantResets = 1
			}
			require.Error(t, runner.run(context.Background(), onDemand))
			require.Equal(t, wantResets, resets)
			require.NotNil(t, runner.pending)
			require.Zero(t, getAuditTrigger(t, c).Status.SuccessfulGeneration)
			require.Error(t, runner.run(context.Background(), true))
			require.Equal(t, 1, runs)
			require.Equal(t, wantResets, resets)
			reject = false
			require.NoError(t, runner.run(context.Background(), true))
			require.NoError(t, runner.run(context.Background(), true))
			require.Equal(t, 1, runs)
			require.Equal(t, wantResets, resets)
			require.EqualValues(t, 7, getAuditTrigger(t, c).Status.SuccessfulGeneration)
		})
	}
}

func TestAuditTriggerSkippedPublicationFailsRequest(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	constraint := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "constraints.gatekeeper.sh/v1beta1",
		"kind":       "TestConstraint",
		"metadata":   map[string]interface{}{"name": "example"},
		"status":     map[string]interface{}{"auditTimestamp": "2026-11-04T00:00:00.002Z"},
	}}
	loop := &updateConstraintLoop{log: logr.Discard()}
	runner := newAuditTriggerRunner(c, func(ctx context.Context) error {
		return loop.updateConstraintStatus(ctx, constraint, newLimitQueue(0), "2026-11-04T00:00:00.001Z", 0)
	})
	require.ErrorContains(t, runner.run(context.Background(), true), "newer than this audit")
	status := getAuditTrigger(t, c).Status
	require.Zero(t, status.SuccessfulGeneration)
	require.EqualValues(t, 7, status.FailedGeneration)
	require.Equal(t, "Failed", status.LastRun.Phase)
	require.Equal(t, map[string]interface{}{"auditTimestamp": "2026-11-04T00:00:00.002Z"}, constraint.Object["status"])
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
	go am.runAuditLoop(ctx, ticks, runner, clock.RealClock{})
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

func TestPeriodicAuditRetriesCompletionWithoutRescanning(t *testing.T) {
	for _, auditErr := range []error{nil, errors.New("audit failed")} {
		name := "success"
		if auditErr != nil {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			trigger := newAuditTrigger(7)
			trigger.Status.SuccessfulGeneration = 7
			c := newAuditTriggerClient(t, trigger)
			var reject atomic.Bool
			reject.Store(true)
			wrapped := interceptor.NewClient(c, interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, name string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if trigger, ok := obj.(*auditv1alpha1.AuditTrigger); ok && trigger.Status.LastRun.Phase != "Running" && reject.Swap(false) {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers/status"}, obj.GetName(), errors.New("denied"))
					}
					return c.SubResource(name).Update(ctx, obj, opts...)
				},
			})
			var runs atomic.Int32
			runner := newAuditTriggerRunner(wrapped, func(context.Context) error {
				runs.Add(1)
				return auditErr
			})
			am := &Manager{stopper: make(chan struct{}), auditTriggerWake: make(chan struct{}, 1)}
			ticks := make(chan time.Time, 1)
			timerClock := clocktesting.NewFakeClock(time.Now())
			ctx, cancel := context.WithCancel(context.Background())
			go am.runAuditLoop(ctx, ticks, runner, timerClock)
			t.Cleanup(func() {
				cancel()
				select {
				case <-am.stopper:
				case <-time.After(5 * time.Second):
					t.Error("audit loop did not stop")
				}
			})
			ticks <- time.Now()
			require.Eventually(t, timerClock.HasWaiters, 5*time.Second, time.Millisecond)
			require.Equal(t, "Running", getAuditTrigger(t, c).Status.LastRun.Phase)
			timerClock.Step(1100 * time.Millisecond)
			require.Eventually(t, func() bool {
				status := getAuditTrigger(t, c).Status
				return status.LastRun.CompletedAt != nil && !timerClock.HasWaiters()
			}, 5*time.Second, time.Millisecond)
			require.EqualValues(t, 1, runs.Load())
			if auditErr != nil {
				require.EqualValues(t, 7, getAuditTrigger(t, c).Status.FailedGeneration)
			}
		})
	}
}

func TestAuditTriggerRetryBackoff(t *testing.T) {
	c := newAuditTriggerClient(t, newAuditTrigger(7))
	var reject atomic.Bool
	reject.Store(true)
	var attempts atomic.Int32
	wrapped := interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if reject.Load() {
				attempts.Add(1)
				return apierrors.NewForbidden(schema.GroupResource{Resource: "audittriggers"}, key.Name, errors.New("denied"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	runner := newAuditTriggerRunner(wrapped, func(context.Context) error { return nil })
	am := &Manager{stopper: make(chan struct{}), auditTriggerWake: make(chan struct{}, 1)}
	timerClock := clocktesting.NewFakeClock(time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	go am.runAuditLoop(ctx, nil, runner, timerClock)
	t.Cleanup(func() {
		cancel()
		select {
		case <-am.stopper:
		case <-time.After(5 * time.Second):
			t.Error("audit loop did not stop")
		}
	})
	waitForRetry := func(want int32) {
		t.Helper()
		require.Eventually(t, func() bool {
			return attempts.Load() == want && timerClock.HasWaiters()
		}, 5*time.Second, time.Millisecond)
	}
	requireNoAttempt := func(want int32) {
		t.Helper()
		require.Never(t, func() bool { return attempts.Load() != want }, 20*time.Millisecond, time.Millisecond)
	}
	am.auditTriggerWake <- struct{}{}
	var count int32 = 1
	waitForRetry(count)
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		timerClock.Step(delay - time.Millisecond)
		requireNoAttempt(count)
		advance := delay/10 + time.Millisecond
		if delay == 30*time.Second {
			advance = time.Millisecond
		}
		timerClock.Step(advance)
		count++
		waitForRetry(count)
	}

	// A watch notification bypasses the pending retry and success cancels it.
	reject.Store(false)
	am.auditTriggerWake <- struct{}{}
	require.Eventually(t, func() bool {
		return getAuditTrigger(t, c).Status.SuccessfulGeneration == 7 && !timerClock.HasWaiters()
	}, 5*time.Second, time.Millisecond)
	reject.Store(true)
	timerClock.Step(time.Hour)
	requireNoAttempt(count)

	am.auditTriggerWake <- struct{}{}
	count++
	waitForRetry(count)
	timerClock.Step(time.Second - time.Millisecond)
	requireNoAttempt(count)
	timerClock.Step(102 * time.Millisecond)
	count++
	waitForRetry(count)
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
