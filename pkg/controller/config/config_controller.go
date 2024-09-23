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

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
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
	ctrlName = "config-controller"
)

var (
	log       = logf.Log.WithName("controller").WithValues("kind", "Config")
	configGVK = configv1alpha1.GroupVersion.WithKind("Config")
)

type Adder struct {
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	CacheManager     *cm.CacheManager
}

// Add creates a new ConfigController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.CacheManager, a.ControllerSwitch, a.Tracker)
	if err != nil {
		return err
	}

	return add(mgr, r)
}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectCacheManager(cm *cm.CacheManager) {
	a.CacheManager = cm
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager, cm *cm.CacheManager, cs *watch.ControllerSwitch, tracker *readiness.Tracker) (*ReconcileConfig, error) {
	if cm == nil {
		return nil, fmt.Errorf("cacheManager must be non-nil")
	}

	return &ReconcileConfig{
		reader:       mgr.GetCache(),
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		scheme:       mgr.GetScheme(),
		cs:           cs,
		cacheManager: cm,
		tracker:      tracker,
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
	err = c.Watch(source.Kind(mgr.GetCache(), &configv1alpha1.Config{}, &handler.TypedEnqueueRequestForObject[*configv1alpha1.Config]{}))
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

	scheme       *runtime.Scheme
	cacheManager *cm.CacheManager
	cs           *watch.ControllerSwitch

	tracker *readiness.Tracker
}

// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=podsecuritypolicies,resourceNames=gatekeeper-admin,verbs=use
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;

// Reconcile reads that state of the cluster for a Config object and makes changes based on the state read
// and what is in the Config.Spec
// Automatically generate RBAC rules to allow the Controller to read all things (for sync).
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

	newExcluder := process.New()
	var statsEnabled bool
	// If the config is being deleted the user is saying they don't want to
	// sync anything
	gvksToSync := []schema.GroupVersionKind{}
	if exists && instance.GetDeletionTimestamp().IsZero() {
		for _, entry := range instance.Spec.Sync.SyncOnly {
			gvk := schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind}
			gvksToSync = append(gvksToSync, gvk)
		}

		newExcluder.Add(instance.Spec.Match)
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

	r.cacheManager.ExcludeProcesses(newExcluder)
	configSourceKey := aggregator.Key{Source: "config", ID: request.NamespacedName.String()}
	if err := r.cacheManager.UpsertSource(ctx, configSourceKey, gvksToSync); err != nil {
		r.tracker.For(configGVK).TryCancelExpect(instance)

		return reconcile.Result{Requeue: true}, fmt.Errorf("config-controller: error establishing watches for new syncOny: %w", err)
	}

	r.tracker.For(configGVK).Observe(instance)
	return reconcile.Result{}, nil
}
