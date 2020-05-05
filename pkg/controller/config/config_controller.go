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
	"time"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	syncc "github.com/open-policy-agent/gatekeeper/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// TODO write a reconciliation process that looks at the state of the cluster to make sure
// allFinalizers agrees with reality and launches a cleaner if the config is missing.

const (
	ctrlName      = "config-controller"
	finalizerName = "finalizers.gatekeeper.sh/config"
)

var CfgKey = types.NamespacedName{Namespace: util.GetNamespace(), Name: "config"}
var log = logf.Log.WithName("controller").WithValues("kind", "Config")

type Adder struct {
	Opa              *opa.Client
	WatchManager     *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
}

// Add creates a new ConfigController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.Opa, a.WatchManager, a.ControllerSwitch)
	if err != nil {
		return err
	}

	return add(mgr, r)
}

func (a *Adder) InjectOpa(o *opa.Client) {
	a.Opa = o
}

func (a *Adder) InjectWatchManager(wm *watch.Manager) {
	a.WatchManager = wm
}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opa syncc.OpaDataClient, wm *watch.Manager, cs *watch.ControllerSwitch) (reconcile.Reconciler, error) {
	watchSet := watch.NewSet()
	filteredOpa := syncc.NewFilteredOpaDataClient(opa, watchSet)
	syncMetricsCache := syncc.NewMetricsCache()

	// Events will be used to receive events from dynamic watches registered
	// via the registrar below.
	events := make(chan event.GenericEvent, 1024)
	syncAdder := syncc.Adder{
		Opa:          filteredOpa,
		Events:       events,
		MetricsCache: syncMetricsCache,
	}
	// Create subordinate controller - we will feed it events dynamically via watch
	if err := syncAdder.Add(mgr); err != nil {
		return nil, fmt.Errorf("registering sync controller: %w", err)
	}

	w, err := wm.NewRegistrar(
		ctrlName,
		events)
	if err != nil {
		return nil, err
	}
	return &ReconcileConfig{
		reader:           mgr.GetCache(),
		writer:           mgr.GetClient(),
		statusClient:     mgr.GetClient(),
		scheme:           mgr.GetScheme(),
		opa:              filteredOpa,
		cs:               cs,
		watcher:          w,
		watched:          watchSet,
		syncMetricsCache: syncMetricsCache,
	}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
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

// ReconcileConfig reconciles a Config object
type ReconcileConfig struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme           *runtime.Scheme
	opa              syncc.OpaDataClient
	syncMetricsCache *syncc.MetricsCache
	cs               *watch.ControllerSwitch
	watcher          *watch.Registrar
	watched          *watch.Set
}

// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs/status,verbs=get;update;patch

// Reconcile reads that state of the cluster for a Config object and makes changes based on the state read
// and what is in the Config.Spec
// Automatically generate RBAC rules to allow the Controller to read all things (for sync)
// update is needed for finalizers
func (r *ReconcileConfig) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Short-circuit if shutting down.
	if r.cs != nil {
		running := r.cs.Enter()
		defer r.cs.Exit()
		if !running {
			return reconcile.Result{}, nil
		}
	}

	// Fetch the Config instance
	if request.NamespacedName != CfgKey {
		log.Info("Ignoring unsupported config name", "namespace", request.NamespacedName.Namespace, "name", request.NamespacedName.Name)
		return reconcile.Result{}, nil
	}
	exists := true
	instance := &configv1alpha1.Config{}
	err := r.reader.Get(context.TODO(), request.NamespacedName, instance)
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
		if err := r.writer.Update(context.Background(), instance); err != nil {
			return reconcile.Result{}, err
		}
	}

	newSyncOnly := watch.NewSet()
	// If the config is being deleted the user is saying they don't want to
	// sync anything
	if exists && instance.GetDeletionTimestamp().IsZero() {
		for _, entry := range instance.Spec.Sync.SyncOnly {
			gvk := schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind}
			newSyncOnly.Add(gvk)
		}
	}

	// If the watch set has not changed, we're done here.
	if r.watched.Equals(newSyncOnly) {
		return reconcile.Result{}, nil
	}

	// --- Start watching the new set ---

	// This must happen first - signals to the opa client in the sync controller
	// to drop events from no-longer-watched resources that may be in its queue.
	needReplay := r.watched.Union(newSyncOnly)
	r.watched.Replace(newSyncOnly)

	// *Note the following steps are not transactional with respect to admission control*

	// Wipe all data to avoid stale state
	if _, err := r.opa.RemoveData(context.Background(), target.WipeData{}); err != nil {
		return reconcile.Result{}, err
	}

	// reset sync cache before sending the metric
	r.syncMetricsCache.ResetCache()
	r.syncMetricsCache.ReportSync(&syncc.Reporter{Ctx: context.TODO()})

	// Important: dynamic watches update must happen *after* updating our watchSet.
	// Otherwise the sync controller will drop events for the newly watched kinds.
	if err := r.watcher.ReplaceWatch(newSyncOnly.Items()); err != nil {
		return reconcile.Result{}, err
	}

	// Replay cached data for any resources that were previously watched and still in the watch set.
	// This is necessary because we wiped their data from Opa above.
	// TODO(OREN): Improve later by selectively removing subtrees of data instead of a full wipe.
	if err := r.replayData(context.TODO(), needReplay); err != nil {
		return reconcile.Result{}, fmt.Errorf("replaying data: %w", err)
	}

	return reconcile.Result{}, nil
}

// replayData replays all watched and cached data into Opa following a config set change.
// In the future we can rework this to avoid the full opa data cache wipe.
func (r *ReconcileConfig) replayData(ctx context.Context, w *watch.Set) error {
	for _, gvk := range w.Items() {
		u := &unstructured.UnstructuredList{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})
		err := r.reader.List(ctx, u)
		if err != nil {
			return fmt.Errorf("replaying data for %+v: %w", gvk, err)
		}

		defer r.syncMetricsCache.ReportSync(&syncc.Reporter{Ctx: context.TODO()})

		for i := range u.Items {
			syncKey := r.syncMetricsCache.GetSyncKey(u.Items[i].GetNamespace(), u.Items[i].GetName())

			if _, err := r.opa.AddData(context.Background(), &u.Items[i]); err != nil {
				r.syncMetricsCache.AddObject(syncKey, syncc.Tags{
					Kind:   u.Items[i].GetKind(),
					Status: metrics.ErrorStatus,
				})
				return fmt.Errorf("adding data for %+v: %w", gvk, err)
			}

			r.syncMetricsCache.AddObject(syncKey, syncc.Tags{
				Kind:   u.Items[i].GetKind(),
				Status: metrics.ActiveStatus,
			})
		}
	}
	return nil
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

// TearDownState removes all finalizers
// written by the config controller
func TearDownState(c client.Client, finished chan struct{}) {
	defer close(finished)
	syncCfg := &configv1alpha1.Config{}
	if err := c.Get(context.Background(), CfgKey, syncCfg); err != nil {
		log.Error(err, "while retrieving sync config")
		return
	}
	cleanFn := func() (bool, error) {
		syncCfg := &configv1alpha1.Config{}
		if err := c.Get(context.Background(), CfgKey, syncCfg); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			log.Error(err, "get error while removing finalizer from config")
			return false, nil
		}
		removeFinalizer(syncCfg)
		if err := c.Update(context.Background(), syncCfg); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			log.Error(err, "write error while removing finalizer from config")
			return false, nil
		}
		return true, nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, cleanFn); err != nil {
		log.Error(err, "max retries for removal of config finalizer reached")
	}
}

func hasFinalizer(instance *configv1alpha1.Config) bool {
	return containsString(finalizerName, instance.GetFinalizers())
}

func removeFinalizer(instance *configv1alpha1.Config) {
	instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
}
