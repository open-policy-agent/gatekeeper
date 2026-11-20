package audittrigger

import (
	"context"
	"errors"

	auditv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/audit/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const ctrlName = "audit-trigger-controller"

var triggerKey = client.ObjectKey{Name: auditv1alpha1.AuditTriggerName}

type reconciler struct {
	client client.Client
	notify func()
}

// +kubebuilder:rbac:groups=audit.gatekeeper.sh,resources=audittriggers,verbs=get;list;watch
// +kubebuilder:rbac:groups=audit.gatekeeper.sh,resources=audittriggers/status,verbs=get;patch;update

// AddToManager registers the controller with the enabled audit process's manager.
// notify must wake the audit scheduler without blocking reconciliation.
func AddToManager(mgr manager.Manager, notify func()) error {
	if notify == nil {
		return errors.New("audit trigger notify callback must be non-nil")
	}
	c, err := controller.New(ctrlName, mgr, controller.Options{
		Reconciler: &reconciler{client: mgr.GetClient(), notify: notify},
	})
	if err != nil {
		return err
	}
	return c.Watch(source.Kind(
		mgr.GetCache(),
		&auditv1alpha1.AuditTrigger{},
		&handler.TypedEnqueueRequestForObject[*auditv1alpha1.AuditTrigger]{},
	))
}

func (r *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if request.NamespacedName != triggerKey {
		return reconcile.Result{}, nil
	}
	trigger := &auditv1alpha1.AuditTrigger{}
	if err := r.client.Get(ctx, triggerKey, trigger); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if !trigger.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}
	if trigger.Status.AcknowledgedGeneration < trigger.Generation {
		trigger.Status.AcknowledgedGeneration = trigger.Generation
		if err := r.client.Status().Update(ctx, trigger); err != nil {
			return reconcile.Result{}, err
		}
	}
	if trigger.Status.SuccessfulGeneration < trigger.Generation &&
		trigger.Status.FailedGeneration < trigger.Generation {
		r.notify()
	}
	return reconcile.Result{}, nil
}
