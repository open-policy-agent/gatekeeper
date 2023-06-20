/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"fmt"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ctrlName      = "config-controller"
	finalizerName = "finalizers.gatekeeper.sh/config"
)

var log = logf.Log.WithName("controller").WithValues("kind", "Config")

type Adder struct {
	Opa              *constraintclient.Client
	WatchManager     *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	ProcessExcluder  *process.Excluder
	WatchSet         *watch.Set
	Events           chan event.GenericEvent
	CacheManager     *cm.CacheManager
}

// Add creates a new ConfigController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.CacheManager, a.WatchManager, a.ControllerSwitch, a.Tracker, a.ProcessExcluder, a.Events, a.WatchSet, a.Events)
	if err != nil {
		return err
	}

	return add(mgr, r)
}

func (a *Adder) InjectOpa(o *constraintclient.Client) {
	a.Opa = o
}

func (a *Adder) InjectWatchManager(wm *watch.Manager) {
	a.WatchManager = wm
}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectProcessExcluder(m *process.Excluder) {
	a.ProcessExcluder = m
}

func (a *Adder) InjectMutationSystem(mutationSystem *mutation.System) {}

func (a *Adder) InjectExpansionSystem(expansionSystem *expansion.System) {}

func (a *Adder) InjectProviderCache(providerCache *externaldata.ProviderCache) {}

func (a *Adder) InjectWatchSet(watchSet *watch.Set) {
	a.WatchSet = watchSet
}

func (a *Adder) InjectEventsCh(events chan event.GenericEvent) {
	a.Events = events
}

func (a *Adder) InjectCacheManager(cm *cm.CacheManager) {
	a.CacheManager = cm
}

// newReconciler returns a new reconcile.Reconciler
// events is the channel from which sync controller will receive the events
// regEvents is the channel registered by Registrar to put the events in
// events and regEvents point to same event channel except for testing.
func newReconciler(mgr manager.Manager, cm *cm.CacheManager, wm *watch.Manager, cs *watch.ControllerSwitch, tracker *readiness.Tracker, processExcluder *process.Excluder, events <-chan event.GenericEvent, watchSet *watch.Set, regEvents chan<- event.GenericEvent) (*ReconcileConfig, error) {
	if watchSet == nil {
		return nil, fmt.Errorf("watchSet must be non-nil")
	}

	w, err := wm.NewRegistrar(
		ctrlName,
		regEvents)
	if err != nil {
		return nil, err
	}

	waca := &WatchAwareCacheAccuator{
		Registrar:    w,
		WatchedSet:   watchSet,
		CacheManager: cm,
	}

	return &ReconcileConfig{
		reader:          mgr.GetCache(),
		writer:          mgr.GetClient(),
		statusClient:    mgr.GetClient(),
		scheme:          mgr.GetScheme(),
		cs:              cs,
		cacheManager:    cm,
		tracker:         tracker,
		processExcluder: processExcluder,
		waca:            waca,
	}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to Config
	err = c.Watch(&source.Kind{Type: &configv1alpha1.Config{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileConfig{}

// ReconcileConfig reconciles a Config object.
type ReconcileConfig struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme          *runtime.Scheme
	cacheManager    *cm.CacheManager
	cs              *watch.ControllerSwitch
	tracker         *readiness.Tracker
	processExcluder *process.Excluder
	waca            *WatchAwareCacheAccuator
}

// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=podsecuritypolicies,resourceNames=gatekeeper-admin,verbs=use
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;

// Reconcile reads that state of the cluster for a Config object and makes changes based on the state read
// and what is in the Config.Spec
// Automatically generate RBAC rules to allow the Controller to read all things (for sync)
// update is needed for finalizers.
func (r *ReconcileConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Short-circuit if shutting down.
	if r.cs != nil {
		running := r.cs.Enter()
		defer r.cs.Exit()
		if !running {
			return reconcile.Result{}, nil
		}
	}

	// Fetch the Config instance
	if request.NamespacedName != keys.Config {
		log.Info("Ignoring unsupported config name", "namespace", request.NamespacedName.Namespace, "name", request.NamespacedName.Name)
		return reconcile.Result{}, nil
	}
	exists := true
	instance := &configv1alpha1.Config{}
	err := r.reader.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		// if config is not found, we should remove cached data
		if errors.IsNotFound(err) {
			exists = false
		} else {
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
	}

	// Actively remove config finalizer. This should automatically remove
	// the finalizer over time even if state teardown didn't work correctly
	// after a deprecation period, all finalizer code can be removed.
	if exists && hasFinalizer(instance) {
		removeFinalizer(instance)
		if err := r.writer.Update(ctx, instance); err != nil {
			return reconcile.Result{}, err
		}
	}

	var statsEnabled bool
	gvks := make([]schema.GroupVersionKind, 0)
	// If the config is being deleted the user is saying they don't want to
	// sync anything
	if exists && instance.GetDeletionTimestamp().IsZero() {
		for _, entry := range instance.Spec.Sync.SyncOnly {
			gvks = append(gvks, schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind})
		}

		statsEnabled = instance.Spec.Readiness.StatsEnabled
	}

	// Enable verbose readiness stats if requested.
	if statsEnabled {
		log.Info("enabling readiness stats")
		r.tracker.EnableStats()
	} else {
		log.Info("disabling readiness stats")
		r.tracker.DisableStats()
	}

	if err := r.waca.HandleGVKsToSync(ctx, gvks, instance.Spec.Match, r.tracker, r.reader); err != nil {
		return reconcile.Result{}, fmt.Errorf("error handling syncOnly update: %w", err)
	}

	return reconcile.Result{}, nil
}

func containsString(s string, items []string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(s string, items []string) []string {
	var rval []string
	for _, item := range items {
		if item != s {
			rval = append(rval, item)
		}
	}
	return rval
}

func hasFinalizer(instance *configv1alpha1.Config) bool {
	return containsString(finalizerName, instance.GetFinalizers())
}

func removeFinalizer(instance *configv1alpha1.Config) {
	instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
}

type WatchAwareCacheAccuator struct {
	Registrar      *watch.Registrar
	WatchedSet     *watch.Set
	needsReplaySet *watch.Set
	needsWipe      bool

	CacheManager *cm.CacheManager
}

func (w *WatchAwareCacheAccuator) HandleGVKsToSync(ctx context.Context, gvks []schema.GroupVersionKind, matchers []configv1alpha1.MatchEntry, tracker *readiness.Tracker, reader client.Reader) error {
	newSyncOnly := watch.NewSet()
	newExcluder := process.New()

	for _, gvk := range gvks {
		newSyncOnly.Add(gvk)
	}
	if matchers != nil {
		newExcluder.Add(matchers)
	}

	// Remove expectations for resources we no longer watch.
	diff := w.WatchedSet.Difference(newSyncOnly)
	for _, gvk := range diff.Items() {
		tracker.CancelData(gvk)
	}

	// If the watch set has not changed, we're done here...
	if w.WatchedSet.Equals(newSyncOnly) {
		// ...unless we have pending wipe / replay operations from a previous reconcile.
		if !(w.needsWipe || w.needsReplaySet != nil) {
			return nil
		}

		// If we reach here, the watch set hasn't changed since last reconcile, but we
		// have unfinished wipe/replay business from the last change.
	} else {
		// The watch set _has_ changed, so recalculate the replay set.
		w.needsReplaySet = nil
		w.needsWipe = true
	}

	// --- Start watching the new set ---

	// This must happen first - signals to the opa client in the sync controller
	// to drop events from no-longer-watched resources that may be in its queue.
	if w.needsReplaySet == nil {
		w.needsReplaySet = w.WatchedSet.Intersection(newSyncOnly)
	}

	// Wipe all data to avoid stale state if needed. Happens once per watch-set-change.
	if err := w.CacheManager.WipeData(ctx); err != nil {
		return fmt.Errorf("wiping opa data cache: %w", err)
	}

	var err error
	w.WatchedSet.Replace(newSyncOnly, func() {
		if matchers != nil {
			// swapping with the new excluder
			w.CacheManager.ReplaceExcluder(newExcluder)
		}

		// *Note the following steps are not transactional with respect to admission control*

		// Important: dynamic watches update must happen *after* updating our watchSet.
		// Otherwise, the sync controller will drop events for the newly watched kinds.
		// Defer error handling so object re-sync happens even if the watch is hard
		// errored due to a missing GVK in the watch set.
		err = w.Registrar.ReplaceWatch(newSyncOnly.Items())
	})
	if err != nil {
		return err
	}

	// Replay cached data for any resources that were previously watched and still in the watch set.
	// This is necessary because we wipe their data from Opa above.
	// TODO(OREN): Improve later by selectively removing subtrees of data instead of a full wipe.
	if err := w.replayData(ctx, reader); err != nil {
		return fmt.Errorf("replaying data: %w", err)
	}

	return nil
}

// replayData replays all watched and cached data into Opa following a config set change.
// In the future we can rework this to avoid the full opa data cache wipe.
func (w *WatchAwareCacheAccuator) replayData(ctx context.Context, reader client.Reader) error {
	if w.needsReplaySet == nil {
		return nil
	}
	for _, gvk := range w.needsReplaySet.Items() {
		u := &unstructured.UnstructuredList{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})
		err := reader.List(ctx, u)
		if err != nil {
			return fmt.Errorf("replaying data for %+v: %w", gvk, err)
		}

		defer w.CacheManager.ReportSyncMetrics()

		for i := range u.Items {
			if err := w.CacheManager.AddObject(ctx, &u.Items[i]); err != nil {
				return fmt.Errorf("adding data for %+v: %w", gvk, err)
			}
		}
		w.needsReplaySet.Remove(gvk)
	}
	w.needsReplaySet = nil
	return nil
}
