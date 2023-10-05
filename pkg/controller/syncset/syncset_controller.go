package syncset

import (
	"context"
	"fmt"

	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ctrlName      = "syncset-controller"
	finalizerName = "finalizers.gatekeeper.sh/syncset"
)

var (
	log = logf.Log.WithName("controller").WithValues("kind", "SyncSet", logging.Process, "syncset_controller")

	syncsetGVK = schema.GroupVersionKind{
		Group:   syncsetv1alpha1.GroupVersion.Group,
		Version: syncsetv1alpha1.GroupVersion.Version,
		Kind:    "SyncSet",
	}
)

type Adder struct {
	CacheManager     *cm.CacheManager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
}

// Add creates a new SyncSetController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.CacheManager, a.ControllerSwitch, a.Tracker)
	if err != nil {
		return err
	}

	return add(mgr, r)
}

func (a *Adder) InjectCacheManager(o *cm.CacheManager) {
	a.CacheManager = o
}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func newReconciler(mgr manager.Manager, cm *cm.CacheManager, cs *watch.ControllerSwitch, tracker *readiness.Tracker) (*ReconcileSyncSet, error) {
	if cm == nil {
		return nil, fmt.Errorf("CacheManager must be non-nil")
	}
	if tracker == nil {
		return nil, fmt.Errorf("ReadyTracker must be non-nil")
	}

	return &ReconcileSyncSet{
		reader:       mgr.GetClient(),
		scheme:       mgr.GetScheme(),
		cs:           cs,
		cacheManager: cm,
		tracker:      tracker,
	}, nil
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(source.Kind(mgr.GetCache(), &syncsetv1alpha1.SyncSet{}), &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileSyncSet{}

// ReconcileSyncSet reconciles a SyncSet object.
type ReconcileSyncSet struct {
	reader client.Reader

	scheme       *runtime.Scheme
	cacheManager *cm.CacheManager
	cs           *watch.ControllerSwitch
	tracker      *readiness.Tracker
}

func (r *ReconcileSyncSet) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Short-circuit if shutting down.
	if r.cs != nil {
		running := r.cs.Enter()
		defer r.cs.Exit()
		if !running {
			return reconcile.Result{}, nil
		}
	}

	syncsetTr := r.tracker.For(syncsetGVK)
	exists := true
	syncset := &syncsetv1alpha1.SyncSet{}
	err := r.reader.Get(ctx, request.NamespacedName, syncset)
	if err != nil {
		if errors.IsNotFound(err) {
			exists = false
		} else {
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
	}

	gvks := make([]schema.GroupVersionKind, 0)
	if exists && syncset.GetDeletionTimestamp().IsZero() {
		log.V(logging.DebugLevel).Info("handling SyncSet update", "instance", syncset)

		for _, entry := range syncset.Spec.GVKs {
			gvks = append(gvks, schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind})
		}
	}

	sk := aggregator.Key{Source: "syncset", ID: request.NamespacedName.String()}
	if err := r.cacheManager.UpsertSource(ctx, sk, gvks); err != nil {
		return reconcile.Result{Requeue: true}, fmt.Errorf("synceset-controller: error changing watches: %w", err)
	}

	syncsetTr.Observe(syncset)

	return reconcile.Result{}, nil
}
