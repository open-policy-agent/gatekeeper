package audit

import (
	"context"
	"errors"
	"time"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var auditTriggerKey = types.NamespacedName{Name: auditv1alpha1.AuditTriggerName}

// The singleton's spec and watermarks are the durable queue. A buffered wake is
// only a hint to reread it, so coalescing and restart recovery need no request IDs.
type auditTriggerRunner struct {
	reader      client.Reader
	client      client.Client
	audit       func(context.Context) error
	now         func() time.Time
	resetTicker func()
	pending     *auditTriggerCompletion
}

type auditTriggerCompletion struct {
	uid    types.UID
	status auditv1alpha1.AuditTriggerStatus
}

func (r *auditTriggerRunner) run(ctx context.Context, onDemand bool) error {
	source := "scheduled"
	if onDemand {
		source = "on-demand"
	}
	ctx = logf.IntoContext(ctx, log.WithValues("auditSource", source))
	if err := r.flushCompletion(ctx); err != nil {
		if onDemand {
			return err
		}
		logf.FromContext(ctx).Error(err, "persisting previous audit completion; continuing periodic audit")
	}
	trigger := &auditv1alpha1.AuditTrigger{}
	if err := r.reader.Get(ctx, auditTriggerKey, trigger); err != nil {
		if !apierrors.IsNotFound(err) {
			if !onDemand {
				return errors.Join(err, r.audit(ctx))
			}
			return err
		}
		if onDemand {
			return nil
		}
		return r.audit(ctx)
	}
	if !trigger.DeletionTimestamp.IsZero() {
		if onDemand {
			return nil
		}
		return r.audit(ctx)
	}
	generation := trigger.Generation
	if onDemand && (trigger.Status.SuccessfulGeneration >= generation ||
		trigger.Status.FailedGeneration >= generation) {
		return nil
	}

	run := auditv1alpha1.AuditRunStatus{
		CoveredGeneration: generation,
		StartedAt:         metav1.NewTime(r.now()),
		Phase:             "Running",
		Reason:            "AuditRunning",
		Message:           "The audit is running",
	}
	if err := r.updateStatus(ctx, trigger.UID, func(status *auditv1alpha1.AuditTriggerStatus) {
		status.AcknowledgedGeneration = max(status.AcknowledgedGeneration, generation)
		status.LastRun = &run
	}); err != nil {
		if onDemand {
			return err
		}
		logf.FromContext(ctx).Error(err, "recording audit start; continuing periodic audit")
	}

	auditErr := r.audit(ctx)
	if ctx.Err() != nil {
		// Cancellation does not establish a terminal result. Recovery reruns work
		// whose completion was not durably recorded.
		return ctx.Err()
	}
	completed := metav1.NewTime(r.now())
	run.CompletedAt = &completed
	run.Phase, run.Reason, run.Message = "Succeeded", "AuditSucceeded", "The audit completed successfully"
	if auditErr != nil {
		run.Phase, run.Reason, run.Message = "Failed", "AuditFailed", truncateString(auditErr.Error(), msgSize)
	}
	if r.pending == nil || r.pending.uid != trigger.UID {
		r.pending = &auditTriggerCompletion{uid: trigger.UID}
	}
	r.pending.status.LastRun = &run
	if auditErr == nil {
		r.pending.status.SuccessfulGeneration = max(r.pending.status.SuccessfulGeneration, generation)
	} else {
		r.pending.status.FailedGeneration = max(r.pending.status.FailedGeneration, generation)
	}
	if onDemand && auditErr == nil && r.resetTicker != nil {
		r.resetTicker()
	}
	return errors.Join(auditErr, r.flushCompletion(ctx))
}

func (r *auditTriggerRunner) flushCompletion(ctx context.Context) error {
	if r.pending == nil {
		return nil
	}
	pending := r.pending.status
	err := r.updateStatus(ctx, r.pending.uid, func(status *auditv1alpha1.AuditTriggerStatus) {
		status.SuccessfulGeneration = max(status.SuccessfulGeneration, pending.SuccessfulGeneration)
		status.FailedGeneration = max(status.FailedGeneration, pending.FailedGeneration)
		status.LastRun = pending.LastRun
	})
	if err == nil {
		r.pending = nil
	}
	return err
}

func (r *auditTriggerRunner) updateStatus(ctx context.Context, uid types.UID, update func(*auditv1alpha1.AuditTriggerStatus)) error {
	// Bound both API calls and retries. Unpersisted completion watermarks are
	// retained in one accumulator so periodic scans can continue during failures.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	backoff := wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   2,
		Jitter:   0.1,
		Steps:    4,
		Cap:      800 * time.Millisecond,
	}
	return backoff.DelayFunc().Until(ctx, true, true, func(ctx context.Context) (bool, error) {
		trigger := &auditv1alpha1.AuditTrigger{}
		if err := r.reader.Get(ctx, auditTriggerKey, trigger); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, auditTriggerRetryError(err)
		}
		if trigger.UID != uid || !trigger.DeletionTimestamp.IsZero() {
			return true, nil
		}
		update(&trigger.Status)
		if err := r.client.Status().Update(ctx, trigger); err != nil {
			return false, auditTriggerRetryError(err)
		}
		return true, nil
	})
}

func auditTriggerRetryError(err error) error {
	if apierrors.IsConflict(err) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) || apierrors.IsServiceUnavailable(err) {
		return nil
	}
	return err
}
