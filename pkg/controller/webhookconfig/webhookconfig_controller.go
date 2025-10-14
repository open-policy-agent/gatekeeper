package webhookconfig

import (
	"context"
	"errors"
	"sync"

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
	"k8s.io/client-go/util/retry"
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

// vwhNameMu protects access to webhook.VwhName for concurrent reads/writes.
var vwhNameMu sync.RWMutex

// getVwhName safely reads webhook.VwhName with synchronization.
func getVwhName() string {
	vwhNameMu.RLock()
	defer vwhNameMu.RUnlock()
	if webhook.VwhName == nil {
		return ""
	}
	return *webhook.VwhName
}

// setVwhName safely writes to webhook.VwhName with synchronization.
// This is primarily for testing purposes.
func setVwhName(name string) {
	vwhNameMu.Lock()
	defer vwhNameMu.Unlock()
	webhook.VwhName = &name
}

var logger = log.Log.V(logging.DebugLevel).WithName("controller").WithValues("kind", "ValidatingWebhookConfiguration", logging.Process, "webhook_config_controller")

// markDirtyTemplate marks a specific constraint template for reconciliation.
func (r *ReconcileWebhookConfig) markDirtyTemplate(template *v1beta1.ConstraintTemplate) {
	r.dirtyMu.Lock()
	defer r.dirtyMu.Unlock()
	if r.dirtyTemplates == nil {
		r.dirtyTemplates = make(map[string]*v1beta1.ConstraintTemplate)
	}
	r.dirtyTemplates[template.Name] = template
	logger.V(1).Info("marked constraint template as dirty", "template", template.Name)
}

// getDirtyTemplatesAndClear returns dirty templates and clears the dirty state.
func (r *ReconcileWebhookConfig) getDirtyTemplatesAndClear() []*v1beta1.ConstraintTemplate {
	r.dirtyMu.Lock()
	defer r.dirtyMu.Unlock()
	if len(r.dirtyTemplates) == 0 {
		return nil
	}
	templates := make([]*v1beta1.ConstraintTemplate, 0, len(r.dirtyTemplates))
	for _, template := range r.dirtyTemplates {
		templates = append(templates, template)
	}
	r.dirtyTemplates = make(map[string]*v1beta1.ConstraintTemplate)
	return templates
}

// triggerConstraintTemplateReconciliation sends events to trigger CT reconciliation for all templates.
func (r *ReconcileWebhookConfig) triggerConstraintTemplateReconciliation(ctx context.Context) error {
	logger.Info("Triggering ConstraintTemplate reconciliation due to webhook matching field changes")

	templateList := &v1beta1.ConstraintTemplateList{}
	if err := r.List(ctx, templateList); err != nil {
		logger.Error(err, "failed to list ConstraintTemplates for webhook reconciliation")
		return err
	}

	var errs []error
	for i := range templateList.Items {
		generateVap, err := constrainttemplate.ShouldGenerateVAPForVersionedCT(&templateList.Items[i], r.scheme)
		if err != nil || !generateVap {
			logger.Info("skipping reconcile for template", "template", templateList.Items[i].GetName())
			continue
		}

		if err := r.sendEventWithRetry(ctx, &templateList.Items[i]); err != nil {
			errs = append(errs, err)
			r.markDirtyTemplate(&templateList.Items[i])
		}
	}
	return errors.Join(errs...)
}

// triggerDirtyTemplateReconciliation sends events only for dirty constraint templates.
func (r *ReconcileWebhookConfig) triggerDirtyTemplateReconciliation(ctx context.Context) error {
	dirtyTemplates := r.getDirtyTemplatesAndClear()
	if len(dirtyTemplates) == 0 {
		return nil
	}

	logger.Info("Triggering reconciliation for dirty ConstraintTemplates", "count", len(dirtyTemplates))

	var errs []error
	for _, template := range dirtyTemplates {
		if err := r.sendEventWithRetry(ctx, template); err != nil {
			errs = append(errs, err)
			r.markDirtyTemplate(template)
		}
	}
	return errors.Join(errs...)
}

func (r *ReconcileWebhookConfig) sendEventWithRetry(ctx context.Context, template *v1beta1.ConstraintTemplate) error {
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return err != nil
	}, func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r.ctEvents <- event.GenericEvent{Object: template}:
			logger.V(1).Info("event sent successfully", "template", template.Name)
			return nil
		default:
			logger.V(1).Info("channel full, will retry with backoff", "template", template.Name)
			return &ChannelFullError{}
		}
	})
}

type ChannelFullError struct{}

func (e *ChannelFullError) Error() string {
	return "channel is full"
}

func (e *ChannelFullError) Temporary() bool {
	return true
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

	// dirtyMu protects access to dirtyTemplates
	dirtyMu        sync.Mutex
	dirtyTemplates map[string]*v1beta1.ConstraintTemplate
} // +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch

// Reconcile processes ValidatingWebhookConfiguration changes.
func (r *ReconcileWebhookConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var configChanged bool

	// Fetch the ValidatingWebhookConfiguration
	webhookConfig := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err := r.Get(ctx, request.NamespacedName, webhookConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Webhook was deleted, remove from cache and trigger reconciliation for all templates
			logger.Info("ValidatingWebhookConfiguration deleted, triggering reconciliation for all ConstraintTemplates")
			r.cache.RemoveConfig(request.Name)
			configChanged = true
		} else {
			return reconcile.Result{}, err
		}
	} else {
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

		newConfig := webhookconfigcache.WebhookMatchingConfig{
			NamespaceSelector: gatekeeperWebhook.NamespaceSelector,
			ObjectSelector:    gatekeeperWebhook.ObjectSelector,
			Rules:             gatekeeperWebhook.Rules,
			MatchPolicy:       gatekeeperWebhook.MatchPolicy,
			MatchConditions:   gatekeeperWebhook.MatchConditions,
		}

		if r.cache.UpsertConfig(request.Name, newConfig) {
			logger.Info("ValidatingWebhookConfiguration matching fields changed", "storedKey", request.Name)
			configChanged = true
		}
	}

	if configChanged {
		// Config changed: reconcile all constraint templates
		if err := r.triggerConstraintTemplateReconciliation(ctx); err != nil {
			logger.Error(err, "failed to trigger ConstraintTemplate reconciliation")
			return reconcile.Result{}, err
		}
	} else {
		// No config change: reconcile only dirty templates
		if err := r.triggerDirtyTemplateReconciliation(ctx); err != nil {
			logger.Error(err, "failed to trigger dirty ConstraintTemplate reconciliation")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// isGatekeeperValidatingWebhook checks if this is a Gatekeeper validating webhook.
func isGatekeeperValidatingWebhook(name string) bool {
	return name == getVwhName()
}

// generateVap determines whether a ConstraintTemplate should generate a ValidatingAdmissionPolicy.
func generateVap(template *v1beta1.ConstraintTemplate) bool {
	generateVAP := false
	if len(template.Spec.Targets) != 1 {
		return generateVAP
	}
	for _, code := range template.Spec.Targets[0].Code {
		if code.Engine != schema.Name {
			continue
		}
		// extract GenerateVAP field form the source
		generateVAP = true
	}
	return generateVAP && *constraint.DefaultGenerateVAP
}
