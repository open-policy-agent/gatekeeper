package externaldata

import (
	"context"

	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
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
	GetPod        func(context.Context) (*corev1.Pod, error)
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

func (a *Adder) InjectGetPod(f func(context.Context) (*corev1.Pod, error)) {
	a.GetPod = f
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
	getPod        func(context.Context) (*corev1.Pod, error)
	scheme        *runtime.Scheme
	reporter      StatsReporter
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager, client *constraintclient.Client, providerCache *frameworksexternaldata.ProviderCache, tracker *readiness.Tracker, getPod func(context.Context) (*corev1.Pod, error)) *Reconciler {
	reporter, err := NewStatsReporter()
	if err != nil {
		log.Error(err, "failed to create stats reporter")
	}
	
	r := &Reconciler{
		cfClient:      client,
		providerCache: providerCache,
		Client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		tracker:       tracker,
		getPod:        getPod,
		reporter:      reporter,
	}
	if getPod == nil {
		r.getPod = r.defaultGetPod
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

	// Watch for changes to Provider
	return c.Watch(
		source.Kind(mgr.GetCache(), &externaldatav1beta1.Provider{},
			&handler.TypedEnqueueRequestForObject[*externaldatav1beta1.Provider]{}))
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile", "request", request)

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
		return reconcile.Result{}, err
	}

	if !deleted {
		if err := r.providerCache.Upsert(unversionedProvider); err != nil {
			log.Error(err, "Upsert failed", "resource", request.NamespacedName)
			tracker.TryCancelExpect(provider)
			// Update status with error before returning
			if statusErr := r.reportProviderError(ctx, provider, statusv1beta1.UpsertCacheError, err.Error()); statusErr != nil {
				log.Error(statusErr, "Failed to report provider error")
			}
			return reconcile.Result{}, err
		}
		tracker.Observe(provider)
		// Update status to reflect successful upsert
		if statusErr := r.reportProviderSuccess(ctx, provider); statusErr != nil {
			log.Error(statusErr, "Failed to report provider success")
			// Don't fail the reconcile for status errors
		}
	} else {
		r.providerCache.Remove(provider.Name)
		tracker.CancelExpect(provider)
		// Clean up ProviderPodStatus on deletion
		if statusErr := r.cleanupProviderPodStatus(ctx, provider); statusErr != nil {
			log.Error(statusErr, "Failed to cleanup provider pod status")
		}
	}

	return ctrl.Result{}, nil
}

// reportProviderError creates or updates a ProviderPodStatus with error information
func (r *Reconciler) reportProviderError(ctx context.Context, provider *externaldatav1beta1.Provider, errorType statusv1beta1.ProviderErrorType, message string) error {
	// Report metrics
	if r.reporter != nil {
		if err := r.reporter.ReportProviderError(ctx, provider.Name, errorType); err != nil {
			log.Error(err, "failed to report provider error metric")
		}
	}

	pod, err := r.getPod(ctx)
	if err != nil {
		return err
	}

	statusObj, err := r.getOrCreateProviderPodStatus(ctx, pod, provider)
	if err != nil {
		return err
	}

	now := metav1.Now()
	statusObj.Status.Active = false
	statusObj.Status.LastTransitionTime = &now
	statusObj.Status.ObservedGeneration = provider.GetGeneration()

	// Add or update error
	found := false
	for i, existingErr := range statusObj.Status.Errors {
		if existingErr.Type == errorType {
			statusObj.Status.Errors[i].Message = message
			statusObj.Status.Errors[i].ErrorTimestamp = &now
			found = true
			break
		}
	}
	if !found {
		statusObj.Status.Errors = append(statusObj.Status.Errors, statusv1beta1.ProviderError{
			Type:           errorType,
			Message:        message,
			Retryable:      true,
			ErrorTimestamp: &now,
		})
	}

	return r.Update(ctx, statusObj)
}

// reportProviderSuccess creates or updates a ProviderPodStatus with success information
func (r *Reconciler) reportProviderSuccess(ctx context.Context, provider *externaldatav1beta1.Provider) error {
	pod, err := r.getPod(ctx)
	if err != nil {
		return err
	}

	statusObj, err := r.getOrCreateProviderPodStatus(ctx, pod, provider)
	if err != nil {
		return err
	}

	now := metav1.Now()
	statusObj.Status.Active = true
	statusObj.Status.LastTransitionTime = &now
	statusObj.Status.LastCacheUpdateTime = &now
	statusObj.Status.ObservedGeneration = provider.GetGeneration()
	statusObj.Status.Errors = nil // Clear errors on success

	return r.Update(ctx, statusObj)
}

// getOrCreateProviderPodStatus gets an existing ProviderPodStatus or creates a new one
func (r *Reconciler) getOrCreateProviderPodStatus(ctx context.Context, pod *corev1.Pod, provider *externaldatav1beta1.Provider) (*statusv1beta1.ProviderPodStatus, error) {
	statusObj := &statusv1beta1.ProviderPodStatus{}
	name := statusv1beta1.KeyForProvider(pod.Name, provider.Name)
	key := types.NamespacedName{Name: name, Namespace: util.GetNamespace()}

	if err := r.Get(ctx, key, statusObj); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		// Create new status object
		var createErr error
		statusObj, createErr = statusv1beta1.NewProviderStatusForPod(pod, provider.Name, provider.GetUID(), r.scheme)
		if createErr != nil {
			return nil, createErr
		}

		// Set labels for controller correlation
		if statusObj.Labels == nil {
			statusObj.Labels = make(map[string]string)
		}
		statusObj.Labels[statusv1beta1.ProviderNameLabel] = provider.Name
		statusObj.Labels[statusv1beta1.PodLabel] = pod.Name

		if err := r.Create(ctx, statusObj); err != nil {
			return nil, err
		}
	}

	return statusObj, nil
}

// cleanupProviderPodStatus removes ProviderPodStatus objects for a deleted provider
func (r *Reconciler) cleanupProviderPodStatus(ctx context.Context, provider *externaldatav1beta1.Provider) error {
	statusList := &statusv1beta1.ProviderPodStatusList{}
	if err := r.List(ctx, statusList,
		client.MatchingLabels{statusv1beta1.ProviderNameLabel: provider.Name},
		client.InNamespace(util.GetNamespace()),
	); err != nil {
		return err
	}

	for _, status := range statusList.Items {
		if err := r.Delete(ctx, &status); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *Reconciler) defaultGetPod(context.Context) (*corev1.Pod, error) {
	// require injection of GetPod in order to control what client we use to
	// guarantee we don't inadvertently create a watch
	panic("GetPod must be injected to Reconciler")
}
