package externaldata

import (
	"context"

	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
}

func (a *Adder) InjectCFClient(c *constraintclient.Client) {
	a.CFClient = c
}

func (a *Adder) InjectControllerSwitch(_ *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectProviderCache(providerCache *frameworksexternaldata.ProviderCache) {
	a.ProviderCache = providerCache
}

// Add creates a new ExternalData Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.CFClient, a.ProviderCache, a.Tracker)
	return add(mgr, r)
}

// Reconciler reconciles a ExternalData object.
type Reconciler struct {
	client.Client
	cfClient      *constraintclient.Client
	providerCache *frameworksexternaldata.ProviderCache
	tracker       *readiness.Tracker
	scheme        *runtime.Scheme
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager, client *constraintclient.Client, providerCache *frameworksexternaldata.ProviderCache, tracker *readiness.Tracker) *Reconciler {
	r := &Reconciler{
		cfClient:      client,
		providerCache: providerCache,
		Client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		tracker:       tracker,
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
				Name: request.NamespacedName.Name,
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
			return reconcile.Result{}, err
		}
		tracker.Observe(provider)
	} else {
		r.providerCache.Remove(provider.Name)
		tracker.CancelExpect(provider)
	}

	return ctrl.Result{}, nil
}
