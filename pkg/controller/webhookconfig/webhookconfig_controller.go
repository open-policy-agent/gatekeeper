package webhookconfig

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constrainttemplate"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ctrlName = "webhookconfig-controller"
)

var logger = log.Log.V(logging.DebugLevel).WithName("controller").WithValues("kind", "ValidatingWebhookConfiguration", logging.Process, "webhook_config_controller")

// TriggerConstraintTemplateReconciliation sends events to trigger CT reconciliation.
func (r *ReconcileWebhookConfig) TriggerConstraintTemplateReconciliation(ctx context.Context, webhookName string) {
	logger := logger.WithValues("webhook_name", webhookName)
	logger.Info("Triggering ConstraintTemplate reconciliation due to webhook matching field changes")

	templateList := &v1beta1.ConstraintTemplateList{}
	if err := r.List(ctx, templateList); err != nil {
		logger.Error(err, "failed to list ConstraintTemplates for webhook reconciliation")
		return
	}

	// Send generic events for each constraint template
	for i := range templateList.Items {
		generateVap, err := constrainttemplate.ShouldGenerateVAPForVersionedCT(&templateList.Items[i], r.scheme)
		if err != nil || !generateVap {
			logger.Info("skipping reconcile for template", "template", templateList.Items[i].GetName())
			continue
		}
		select {
		case r.ctEvents <- event.GenericEvent{
			Object: &templateList.Items[i],
		}:
		default:
			logger.Info("constraint template event channel full, skipping", "template", templateList.Items[i].GetName())
		}
	}

	logger.Info("triggered reconciliation for ConstraintTemplates", "count", len(templateList.Items))
}

type Adder struct {
	Cache    *webhookconfigcache.WebhookConfigCache
	ctEvents chan<- event.GenericEvent // channel to send CT reconciliation events
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

func (a *Adder) InjectWebhookConfigCache(webhookConfigCache *webhookconfigcache.WebhookConfigCache) {
	a.Cache = webhookConfigCache
}

func (a *Adder) InjectConstraintTemplateEvent(ctEvents chan event.GenericEvent) {
	a.ctEvents = ctEvents
}

// Add creates a new webhook config controller and adds it to the Manager.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.IsAssigned(operations.Generate) || !*transform.SyncVAPScope {
		return nil
	}
	r := &ReconcileWebhookConfig{
		Client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		cache:    a.Cache,
		ctEvents: a.ctEvents,
	}

	return add(mgr, r)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to ValidatingWebhookConfiguration with predicate for Gatekeeper webhook only
	err = c.Watch(
		source.Kind(mgr.GetCache(), &admissionregistrationv1.ValidatingWebhookConfiguration{},
			&handler.TypedEnqueueRequestForObject[*admissionregistrationv1.ValidatingWebhookConfiguration]{},
			predicate.TypedFuncs[*admissionregistrationv1.ValidatingWebhookConfiguration]{
				CreateFunc: func(e event.TypedCreateEvent[*admissionregistrationv1.ValidatingWebhookConfiguration]) bool {
					return isGatekeeperValidatingWebhook(e.Object.GetName())
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*admissionregistrationv1.ValidatingWebhookConfiguration]) bool {
					return isGatekeeperValidatingWebhook(e.ObjectNew.GetName())
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*admissionregistrationv1.ValidatingWebhookConfiguration]) bool {
					return isGatekeeperValidatingWebhook(e.Object.GetName())
				},
			}))
	if err != nil {
		return err
	}

	return nil
}

// ReconcileWebhookConfig reconciles ValidatingWebhookConfiguration changes.
type ReconcileWebhookConfig struct {
	client.Client
	scheme   *runtime.Scheme
	cache    *webhookconfigcache.WebhookConfigCache
	ctEvents chan<- event.GenericEvent
}

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch

// Reconcile processes ValidatingWebhookConfiguration changes.
func (r *ReconcileWebhookConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := logger.WithValues("webhook_config", request.Name)

	// Fetch the ValidatingWebhookConfiguration
	webhookConfig := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err := r.Get(ctx, request.NamespacedName, webhookConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Webhook was deleted, remove from cache and trigger reconciliation
			logger.Info("ValidatingWebhookConfiguration deleted, triggering ConstraintTemplate reconciliation")
			r.cache.RemoveConfig(request.Name)
			r.TriggerConstraintTemplateReconciliation(ctx, request.Name)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	var gatekeeperWebhook *admissionregistrationv1.ValidatingWebhook
	for i := range webhookConfig.Webhooks {
		if webhookConfig.Webhooks[i].Name == webhook.ValidatingWebhookName {
			gatekeeperWebhook = &webhookConfig.Webhooks[i]
			break
		}
	}

	if gatekeeperWebhook == nil {
		logger.Info("webhook not found", "name", webhook.ValidatingWebhookName)
		return reconcile.Result{}, nil
	}

	// Extract matching configuration
	newConfig := webhookconfigcache.WebhookMatchingConfig{
		NamespaceSelector: gatekeeperWebhook.NamespaceSelector,
		ObjectSelector:    gatekeeperWebhook.ObjectSelector,
		Rules:             gatekeeperWebhook.Rules,
		MatchPolicy:       gatekeeperWebhook.MatchPolicy,
		MatchConditions:   gatekeeperWebhook.MatchConditions,
	}

	// Check if matching fields have changed
	if r.cache.UpdateConfig(request.Name, newConfig) {
		logger.Info("ValidatingWebhookConfiguration matching fields changed, triggering ConstraintTemplate reconciliation", "storedKey", request.Name)
		r.TriggerConstraintTemplateReconciliation(ctx, request.Name)
	}

	return reconcile.Result{}, nil
}

// isGatekeeperValidatingWebhook checks if this is a Gatekeeper validating webhook.
func isGatekeeperValidatingWebhook(name string) bool {
	return name == *webhook.VwhName
}
