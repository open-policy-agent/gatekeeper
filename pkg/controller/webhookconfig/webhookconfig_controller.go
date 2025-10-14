package webhookconfig

import (
	"context"
	"reflect"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// WebhookMatchingConfig represents the fields that affect resource matching in a webhook.
type WebhookMatchingConfig struct {
	NamespaceSelector *metav1.LabelSelector                        `json:"namespaceSelector,omitempty"`
	ObjectSelector    *metav1.LabelSelector                        `json:"objectSelector,omitempty"`
	Rules             []admissionregistrationv1.RuleWithOperations `json:"rules,omitempty"`
	MatchPolicy       *admissionregistrationv1.MatchPolicyType     `json:"matchPolicy,omitempty"`
	MatchConditions   []admissionregistrationv1.MatchCondition     `json:"matchConditions,omitempty"`
}

// WebhookConfigCache maintains the current state of webhook configurations.
type WebhookConfigCache struct {
	mu      sync.RWMutex
	configs map[string]WebhookMatchingConfig // webhook name -> config
}

// NewWebhookConfigCache creates a new webhook config cache.
func NewWebhookConfigCache(ctEvents <-chan event.GenericEvent) *WebhookConfigCache {
	return &WebhookConfigCache{
		configs: make(map[string]WebhookMatchingConfig),
	}
}

// UpdateConfig updates the cached config and returns whether it changed.
func (w *WebhookConfigCache) UpdateConfig(webhookName string, newConfig WebhookMatchingConfig) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	logger := log.Log.WithName("webhook-config-cache")
	logger.Info("storing webhook config in cache", "key", webhookName, "hasNamespaceSelector", newConfig.NamespaceSelector != nil)

	oldConfig, exists := w.configs[webhookName]
	if !exists || !reflect.DeepEqual(oldConfig, newConfig) {
		w.configs[webhookName] = newConfig
		logger.Info("webhook config stored/updated in cache", "key", webhookName, "cacheSize", len(w.configs))
		return true
	}
	return false
}

// RemoveConfig removes a webhook config from cache.
func (w *WebhookConfigCache) RemoveConfig(webhookName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.configs, webhookName)
}

// GetConfig retrieves the current webhook configuration from cache.
func (w *WebhookConfigCache) GetConfig(webhookName string) (WebhookMatchingConfig, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	logger := log.Log.WithName("webhook-config-cache")
	logger.Info("retrieving webhook config from cache", "key", webhookName, "cacheSize", len(w.configs))

	config, exists := w.configs[webhookName]
	logger.Info("webhook config lookup result", "key", webhookName, "exists", exists)
	return config, exists
}

// TriggerConstraintTemplateReconciliation sends events to trigger CT reconciliation.
func (r *ReconcileWebhookConfig) TriggerConstraintTemplateReconciliation(ctx context.Context, webhookName string) {
	logger := logger.WithValues("webhook_name", webhookName)
	logger.Info("Triggering ConstraintTemplate reconciliation due to webhook matching field changes")

	// List all ConstraintTemplates
	// TODO: optimize this by only triggering reconciliation for VAP gen templates
	templateList := &v1beta1.ConstraintTemplateList{}
	if err := r.List(ctx, templateList); err != nil {
		logger.Error(err, "failed to list ConstraintTemplates for webhook reconciliation")
		return
	}

	// Send generic events for each constraint template
	for i := range templateList.Items {
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
	Cache    *WebhookConfigCache
	ctEvents chan<- event.GenericEvent // channel to send CT reconciliation events
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

func (a *Adder) InjectWebhookConfigCache(webhookConfigCache *WebhookConfigCache) {
	a.Cache = webhookConfigCache
}

func (a *Adder) InjectConstraintTemplateEvent(ctEvents chan event.GenericEvent) {
	a.ctEvents = ctEvents
}

// Add creates a new webhook config controller and adds it to the Manager.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.IsAssigned(operations.Generate) {
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
	cache    *WebhookConfigCache
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
			// TODO: what happens if the webhook is deleted?
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
		logger.Info("webhook not found in configuration", "name", webhook.ValidatingWebhookName)
		// TODO: what happens if webhook is not found?
		return reconcile.Result{}, nil
	}

	// Extract matching configuration
	newConfig := WebhookMatchingConfig{
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
	} else {
		logger.V(1).Info("ValidatingWebhookConfiguration updated but no matching field changes detected", "storedKey", request.Name)
	}

	return reconcile.Result{}, nil
}

// isGatekeeperValidatingWebhook checks if this is a Gatekeeper validating webhook.
func isGatekeeperValidatingWebhook(name string) bool {
	return name == *webhook.VwhName
}
