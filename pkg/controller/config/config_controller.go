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
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/configstatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(context.Context) (*corev1.Pod, error)
}

// Add creates a new ConfigController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.CacheManager, a.ControllerSwitch, a.Tracker, a.GetPod)
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

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager, cm *cm.CacheManager, cs *watch.ControllerSwitch, tracker *readiness.Tracker, getPod func(context.Context) (*corev1.Pod, error)) (*ReconcileConfig, error) {
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
		getPod:       getPod,
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

	err = c.Watch(
		source.Kind(mgr.GetCache(), &statusv1beta1.ConfigPodStatus{}, handler.TypedEnqueueRequestsFromMapFunc(configstatus.PodStatusToConfigMapper(true))))
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

	getPod func(context.Context) (*corev1.Pod, error)
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
		if apierrors.IsNotFound(err) {
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

	// K8s API conventions consider an object to be deleted when either the object no longer exists or when a deletion timestamp has been set.
	deleted := !exists || !instance.GetDeletionTimestamp().IsZero()

	if !deleted {
		for _, entry := range instance.Spec.Sync.SyncOnly {
			gvksToSync = append(gvksToSync, entry.ToGroupVersionKind())
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

		return reconcile.Result{Requeue: true}, r.updateOrCreatePodStatus(ctx, instance, err)
	}

	r.tracker.For(configGVK).Observe(instance)

	if deleted {
		return reconcile.Result{}, r.deleteStatus(ctx, request.NamespacedName.Namespace, request.NamespacedName.Name)
	}
	return reconcile.Result{}, r.updateOrCreatePodStatus(ctx, instance, nil)
}

func (r *ReconcileConfig) deleteStatus(ctx context.Context, cfgNamespace string, cfgName string) error {
	status := &statusv1beta1.ConfigPodStatus{}
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := statusv1beta1.KeyForConfig(pod.Name, cfgNamespace, cfgName)
	if err != nil {
		return fmt.Errorf("getting key for config: %w", err)
	}
	status.SetName(sName)
	status.SetNamespace(util.GetNamespace())
	if err := r.writer.Delete(ctx, status); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ReconcileConfig) updateOrCreatePodStatus(ctx context.Context, cfg *configv1alpha1.Config, upsertErr error) error {
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}

	// Check if it exists already
	sNS := pod.Namespace
	sName, err := statusv1beta1.KeyForConfig(pod.Name, cfg.GetNamespace(), cfg.GetName())
	if err != nil {
		return fmt.Errorf("getting key for config: %w", err)
	}
	shouldCreate := true
	status := &statusv1beta1.ConfigPodStatus{}

	err = r.reader.Get(ctx, types.NamespacedName{Namespace: sNS, Name: sName}, status)
	switch {
	case err == nil:
		shouldCreate = false
	case apierrors.IsNotFound(err):
		if status, err = r.newConfigStatus(pod, cfg); err != nil {
			return fmt.Errorf("creating new config status: %w", err)
		}
	default:
		return fmt.Errorf("getting config status in name %s, namespace %s: %w", cfg.GetName(), cfg.GetNamespace(), err)
	}

	setStatusError(status, upsertErr)

	status.Status.ObservedGeneration = cfg.GetGeneration()

	if shouldCreate {
		return r.writer.Create(ctx, status)
	}
	return r.writer.Update(ctx, status)
}

func (r *ReconcileConfig) newConfigStatus(pod *corev1.Pod, cfg *configv1alpha1.Config) (*statusv1beta1.ConfigPodStatus, error) {
	status, err := statusv1beta1.NewConfigStatusForPod(pod, cfg.GetNamespace(), cfg.GetName(), r.scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	status.Status.ConfigUID = cfg.GetUID()

	return status, nil
}

func setStatusError(status *statusv1beta1.ConfigPodStatus, etErr error) {
	if etErr == nil {
		status.Status.Errors = nil
		return
	}
	e := &statusv1beta1.ConfigError{Message: etErr.Error()}
	status.Status.Errors = []*statusv1beta1.ConfigError{e}
}
