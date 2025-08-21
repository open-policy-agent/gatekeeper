package externaldata

import (
	"context"
	"fmt"
	"time"

	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/externaldatastatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	log = logf.Log.WithName("controller").WithValues(logging.Process, "externaldata_controller")

	gvkExternalData = schema.GroupVersionKind{
		Group:   "externaldata.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "Provider",
	}
)

type Adder struct {
	CFClient      *constraintclient.Client
	ProviderCache *frameworksexternaldata.ProviderCache
	Tracker       *readiness.Tracker
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(context.Context) (*corev1.Pod, error)
}

func (a *Adder) InjectCFClient(c *constraintclient.Client) {
	a.CFClient = c
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectProviderCache(providerCache *frameworksexternaldata.ProviderCache) {
	a.ProviderCache = providerCache
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

// Add creates a new ExternalData Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.CFClient, a.ProviderCache, a.Tracker, a.GetPod)
	return add(mgr, r)
}

// Reconciler reconciles a ExternalData object.
type Reconciler struct {
	client.Client
	cfClient      *constraintclient.Client
	providerCache *frameworksexternaldata.ProviderCache
	tracker       *readiness.Tracker
	scheme        *runtime.Scheme
	metrics *reporter

	getPod func(context.Context) (*corev1.Pod, error)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager, client *constraintclient.Client, providerCache *frameworksexternaldata.ProviderCache, tracker *readiness.Tracker, getPod func(ctx context.Context) (*corev1.Pod, error)) *Reconciler {
	r := &Reconciler{
		cfClient:        client,
		providerCache:   providerCache,
		Client:          mgr.GetClient(),
		scheme:          mgr.GetScheme(),
		tracker:         tracker,
		getPod:          getPod,
		metrics: newStatsReporter(),
	}
	return r
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	if !*externaldata.ExternalDataEnabled {
		return nil
	}

	// Create a new controller
	c, err := controller.New("externaldata-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &statusv1beta1.ProviderPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(externaldatastatus.PodStatusToProviderMapper(true))),
	)
	if err != nil {
		return err
	}

	// Watch for changes to Provider
	return c.Watch(
		source.Kind(mgr.GetCache(), &externaldatav1beta1.Provider{},
			&handler.TypedEnqueueRequestForObject[*externaldatav1beta1.Provider]{}))
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	defer r.metrics.report(ctx)
	log.V(logging.DebugLevel).Info("Reconcile", "request", request)

	deleted := false
	provider := &externaldatav1beta1.Provider{}
	err := r.Get(ctx, request.NamespacedName, provider)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
		provider = &externaldatav1beta1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name: request.Name,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Provider",
				APIVersion: "v1beta1",
			},
		}
	}

	deleted = deleted || !provider.GetDeletionTimestamp().IsZero()
	tracker := r.tracker.For(gvkExternalData)

	unversionedProvider := &externaldataUnversioned.Provider{}
	if err := r.scheme.Convert(provider, unversionedProvider, nil); err != nil {
		log.Error(err, "conversion error")
		providerErrors := []*statusv1beta1.ProviderError{{
			Message: err.Error(),
			Type:   statusv1beta1.ConversionError,
			Retryable: true,
			ErrorTimestamp: &metav1.Time{Time: time.Now()},
		}}
		r.metrics.reportProviderError(ctx)
		r.metrics.add(request.NamespacedName, metrics.ErrorStatus)
		return reconcile.Result{}, r.updateOrCreatePodStatus(ctx, provider, providerErrors)
	}

	if !deleted {
		if err := r.providerCache.Upsert(unversionedProvider); err != nil {
			log.Error(err, "Upsert failed", "resource", request.NamespacedName)
			tracker.TryCancelExpect(provider)
			providerErrors := []*statusv1beta1.ProviderError{{
				Message: err.Error(),
				Type:   statusv1beta1.UpsertCacheError,
				Retryable: true,
				ErrorTimestamp: &metav1.Time{Time: time.Now()},
			}}
			r.metrics.reportProviderError(ctx)
			r.metrics.add(request.NamespacedName, metrics.ErrorStatus)
			return reconcile.Result{}, r.updateOrCreatePodStatus(ctx, provider, providerErrors)
		}
		tracker.Observe(provider)
		r.metrics.add(request.NamespacedName, metrics.ActiveStatus)
		return ctrl.Result{}, r.updateOrCreatePodStatus(ctx, provider, nil)
	}
	r.providerCache.Remove(provider.Name)
	tracker.CancelExpect(provider)
	r.metrics.remove(request.NamespacedName)
	return ctrl.Result{}, r.deleteStatus(ctx, request.Name)
}

func (r *Reconciler) updateOrCreatePodStatus(ctx context.Context, provider *externaldatav1beta1.Provider, providerErrors []*statusv1beta1.ProviderError) error {
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}

	// Check if it exists already
	sNS := pod.Namespace
	sName, err := statusv1beta1.KeyForProvider(pod.Name, provider.GetName())
	if err != nil {
		return fmt.Errorf("getting key for provider: %w", err)
	}
	shouldCreate := true
	status := &statusv1beta1.ProviderPodStatus{}

	err = r.Get(ctx, types.NamespacedName{Namespace: sNS, Name: sName}, status)
	switch {
	case err == nil:
		shouldCreate = false
	case errors.IsNotFound(err):
		if status, err = r.newProviderStatus(pod, provider); err != nil {
			return fmt.Errorf("creating new provider status: %w", err)
		}
	default:
		return fmt.Errorf("getting provider status in name %s, namespace %s: %w", provider.GetName(), provider.GetNamespace(), err)
	}

	if errorChanged(status.Status.Errors, providerErrors) {
		status.Status.LastTransitionTime = &metav1.Time{Time: time.Now()}
	}

	setStatus(status, providerErrors)
	status.Status.ObservedGeneration = provider.GetGeneration()

	if shouldCreate {
		return r.Create(ctx, status)
	}
	return r.Update(ctx, status)
}

func (r *Reconciler) newProviderStatus(pod *corev1.Pod, provider *externaldatav1beta1.Provider) (*statusv1beta1.ProviderPodStatus, error) {
	status, err := statusv1beta1.NewProviderStatusForPod(pod, provider.GetName(), r.scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	status.Status.ProviderUID = provider.GetUID()

	return status, nil
}

func (r *Reconciler) deleteStatus(ctx context.Context, providerName string) error {
	status := &statusv1beta1.ProviderPodStatus{}
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := statusv1beta1.KeyForProvider(pod.Name, providerName)
	if err != nil {
		return fmt.Errorf("getting key for provider: %w", err)
	}
	status.SetName(sName)
	status.SetNamespace(util.GetNamespace())
	if err := r.Delete(ctx, status); err != nil {
		if !errors.IsNotFound(err) {
			return err
		} 
	}
	return nil
}

func setStatus(status *statusv1beta1.ProviderPodStatus, providerErrors []*statusv1beta1.ProviderError) {
	if len(providerErrors) == 0 {
		status.Status.Errors = nil
		status.Status.Active = true
		status.Status.LastCacheUpdateTime = &metav1.Time{Time: time.Now()}
		return
	}

	status.Status.Errors = providerErrors
	status.Status.Active = false
}

func errorChanged(oldErrors, newErrors []*statusv1beta1.ProviderError) bool {
	if len(oldErrors) != len(newErrors) {
		return true
	}
	
	// Check errors without considering order
	oldErrorMap := make(map[string]bool)
	for _, err := range oldErrors {
		key := fmt.Sprintf("%s:%s", err.Type, err.Message)
		oldErrorMap[key] = true
	}
	
	for _, err := range newErrors {
		key := fmt.Sprintf("%s:%s", err.Type, err.Message)
		if !oldErrorMap[key] {
			return true
		}
		delete(oldErrorMap, key)
	}
	
	// If any old errors remain, they weren't found in new errors
	return len(oldErrorMap) > 0
}
