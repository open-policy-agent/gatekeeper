package audit

import (
	"context"
	"fmt"
	"sync"
	"time"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	auditTriggerControllerName = "audit-trigger-controller"

	reasonTriggerAcknowledged = "TriggerAcknowledged"
	reasonAuditInProgress     = "AuditInProgress"
	reasonAuditSucceeded      = "AuditSucceeded"
	reasonAuditFailed         = "AuditFailed"
)

type auditRequest struct {
	name       types.NamespacedName
	uid        types.UID
	generation int64
}

type auditRequestKey struct {
	name       types.NamespacedName
	uid        types.UID
	generation int64
}

func (r auditRequest) key() auditRequestKey {
	return auditRequestKey(r)
}

type auditResult struct {
	err error
}

type auditTriggerReconciler struct {
	client   client.Client
	requests chan<- auditRequest
	pending  *sync.Map
	results  *sync.Map
	now      func() time.Time
}

// +kubebuilder:rbac:groups=audit.gatekeeper.sh,resources=audittriggers,verbs=get;list;watch
// +kubebuilder:rbac:groups=audit.gatekeeper.sh,resources=audittriggers/status,verbs=get;patch;update

func addAuditTriggerController(mgr manager.Manager, am *Manager) error {
	reconciler := &auditTriggerReconciler{
		client:   mgr.GetClient(),
		requests: am.auditRequests,
		pending:  &am.pendingAuditRequests,
		results:  &am.auditResults,
		now:      time.Now,
	}
	c, err := controller.New(auditTriggerControllerName, mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&auditv1alpha1.AuditTrigger{},
		&handler.TypedEnqueueRequestForObject[*auditv1alpha1.AuditTrigger]{},
	)); err != nil {
		return err
	}

	return c.Watch(source.Channel(am.auditTriggerEvents, &handler.EnqueueRequestForObject{}))
}

func (r *auditTriggerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	trigger := &auditv1alpha1.AuditTrigger{}
	if err := r.client.Get(ctx, request.NamespacedName, trigger); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.deleteRequestsForName(request.NamespacedName)
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if !trigger.DeletionTimestamp.IsZero() {
		r.deleteRequestsForName(request.NamespacedName)
		return reconcile.Result{}, nil
	}

	succeeded := meta.FindStatusCondition(trigger.Status.Conditions, auditv1alpha1.ConditionSucceeded)
	if succeeded != nil &&
		succeeded.ObservedGeneration == trigger.Generation &&
		succeeded.Status != metav1.ConditionUnknown {
		r.deleteStaleRequests(trigger)
		return reconcile.Result{}, nil
	}

	acknowledged := meta.FindStatusCondition(trigger.Status.Conditions, auditv1alpha1.ConditionAcknowledged)
	if acknowledged == nil ||
		acknowledged.ObservedGeneration != trigger.Generation ||
		acknowledged.Status != metav1.ConditionTrue ||
		succeeded == nil ||
		succeeded.ObservedGeneration != trigger.Generation {
		meta.SetStatusCondition(&trigger.Status.Conditions, metav1.Condition{
			Type:               auditv1alpha1.ConditionAcknowledged,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: trigger.Generation,
			Reason:             reasonTriggerAcknowledged,
			Message:            "The audit request has been acknowledged",
		})
		meta.SetStatusCondition(&trigger.Status.Conditions, metav1.Condition{
			Type:               auditv1alpha1.ConditionSucceeded,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: trigger.Generation,
			Reason:             reasonAuditInProgress,
			Message:            "The requested audit is pending or in progress",
		})
		if err := r.client.Status().Update(ctx, trigger); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	auditRequest := auditRequest{
		name:       request.NamespacedName,
		uid:        trigger.UID,
		generation: trigger.Generation,
	}
	r.deleteStaleRequests(trigger)

	if result, found := r.results.Load(auditRequest.key()); found {
		auditResult, ok := result.(auditResult)
		if !ok {
			return reconcile.Result{}, fmt.Errorf("unexpected audit result type %T", result)
		}
		condition := metav1.Condition{
			Type:               auditv1alpha1.ConditionSucceeded,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: auditRequest.generation,
			Reason:             reasonAuditSucceeded,
			Message:            "The requested audit completed successfully",
		}
		if auditResult.err != nil {
			condition.Status = metav1.ConditionFalse
			condition.Reason = reasonAuditFailed
			condition.Message = auditResult.err.Error()
		}
		meta.SetStatusCondition(&trigger.Status.Conditions, condition)
		if err := r.client.Status().Update(ctx, trigger); err != nil {
			return reconcile.Result{}, err
		}
		r.results.Delete(auditRequest.key())
		r.pending.Delete(auditRequest.key())
		return reconcile.Result{}, nil
	}

	if delay := trigger.Spec.Timestamp.Sub(r.now()); delay > 0 {
		return reconcile.Result{RequeueAfter: delay}, nil
	}

	if _, loaded := r.pending.LoadOrStore(auditRequest.key(), struct{}{}); loaded {
		return reconcile.Result{}, nil
	}

	select {
	case r.requests <- auditRequest:
		return reconcile.Result{}, nil
	default:
		r.pending.Delete(auditRequest.key())
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}
}

func (r *auditTriggerReconciler) deleteStaleRequests(trigger *auditv1alpha1.AuditTrigger) {
	r.deleteRequests(func(key auditRequestKey) bool {
		return key.name == client.ObjectKeyFromObject(trigger) &&
			(key.uid != trigger.UID || key.generation != trigger.Generation)
	})
}

func (r *auditTriggerReconciler) deleteRequestsForName(name types.NamespacedName) {
	r.deleteRequests(func(key auditRequestKey) bool {
		return key.name == name
	})
}

func (r *auditTriggerReconciler) deleteRequests(shouldDelete func(auditRequestKey) bool) {
	r.pending.Range(func(key, _ any) bool {
		requestKey, ok := key.(auditRequestKey)
		if ok && shouldDelete(requestKey) {
			r.pending.Delete(key)
			r.results.Delete(key)
		}
		return true
	})
	r.results.Range(func(key, _ any) bool {
		requestKey, ok := key.(auditRequestKey)
		if ok && shouldDelete(requestKey) {
			r.results.Delete(key)
			r.pending.Delete(key)
		}
		return true
	})
}

func (am *Manager) drainAuditRequests(first *auditRequest) []auditRequest {
	requests := make([]auditRequest, 0, len(am.auditRequests)+1)
	if first != nil {
		requests = append(requests, *first)
	}
	for {
		select {
		case request := <-am.auditRequests:
			requests = append(requests, request)
		default:
			return requests
		}
	}
}

func (am *Manager) reportAuditResults(ctx context.Context, requests []auditRequest, auditErr error) {
	for _, request := range requests {
		am.auditResults.Store(request.key(), auditResult{err: auditErr})
		select {
		case am.auditTriggerEvents <- event.GenericEvent{Object: &auditv1alpha1.AuditTrigger{
			ObjectMeta: metav1.ObjectMeta{Name: request.name.Name},
		}}:
		case <-ctx.Done():
			return
		}
	}
}
