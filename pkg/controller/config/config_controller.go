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
	"reflect"
	"strings"
	"sync"
	"time"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/pkg/apis/config/v1alpha1"
	syncc "github.com/open-policy-agent/gatekeeper/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// TODO write a reconciliation process that looks at the state of the cluster to make sure
// allFinalizers agrees with reality and launches a cleaner if the config is missing.

const (
	ctrlName      = "config-controller"
	finalizerName = "finalizers.gatekeeper.sh/config"
)

var cfgKey = types.NamespacedName{Namespace: "gatekeeper-system", Name: "config"}
var log = logf.Log.WithName("controller")

type Adder struct {
	Opa          opa.Client
	WatchManager *watch.WatchManager
}

// Add creates a new ConfigController and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.Opa, a.WatchManager)
	if err != nil {
		return err
	}
	return add(mgr, r)
}

func (a *Adder) InjectOpa(o opa.Client) {
	a.Opa = o
}

func (a *Adder) InjectWatchManager(wm *watch.WatchManager) {
	a.WatchManager = wm
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opa opa.Client, wm *watch.WatchManager) (reconcile.Reconciler, error) {
	syncAdder := syncc.Adder{Opa: opa}
	w, err := wm.NewRegistrar(
		ctrlName,
		[]func(manager.Manager, schema.GroupVersionKind) error{syncAdder.Add})
	if err != nil {
		return nil, err
	}
	return &ReconcileConfig{
		Client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		opa:     opa,
		watcher: w,
		watched: newSet(),
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
	client.Client
	scheme  *runtime.Scheme
	opa     opa.Client
	watcher *watch.Registrar
	watched *watchSet
	fc      *finalizerCleanup
}

// Reconcile reads that state of the cluster for a Config object and makes changes based on the state read
// and what is in the Config.Spec
// Automatically generate RBAC rules to allow the Controller to read all things (for sync)
// update is needed for finalizers
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=sync.gatekeeper.sh,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sync.gatekeeper.sh,resources=configs/status,verbs=get;update;patch
func (r *ReconcileConfig) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the Config instance
	if request.NamespacedName != cfgKey {
		log.Info("Ignoring unsupported config name", "namespace", request.NamespacedName.Namespace, "name", request.NamespacedName.Name)
		return reconcile.Result{}, nil
	}
	instance := &configv1alpha1.Config{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	newSyncOnly := newSet()
	toClean := newSet()
	if instance.GetDeletionTimestamp().IsZero() {
		if !containsString(finalizerName, instance.GetFinalizers()) {
			instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
		for _, entry := range instance.Spec.Sync.SyncOnly {
			gvk := schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind}
			newSyncOnly.Add(gvk)
		}
		// Handle deletion
	} else {
		if containsString(finalizerName, instance.GetFinalizers()) {
			instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
		}
	}
	// make sure old finalizers get cleaned up even on restart
	for _, gvk := range instance.Status.AllFinalizers {
		toClean.Add(configv1alpha1.ToGVK(gvk))
	}

	if !r.watched.Equals(newSyncOnly) {
		// Wipe all data to avoid stale state
		err := r.watcher.Pause()
		defer r.watcher.Unpause()
		if err != nil {
			return reconcile.Result{}, err
		}
		if _, err := r.opa.RemoveData(context.Background(), target.WipeData{}); err != nil {
			return reconcile.Result{}, err
		}
	}

	toClean.AddSet(r.watched)
	items := toClean.Items()
	allFinalizers := make([]configv1alpha1.GVK, len(items))
	for i, gvk := range items {
		allFinalizers[i] = configv1alpha1.ToAPIGVK(gvk)
	}
	instance.Status.AllFinalizers = allFinalizers
	toClean.RemoveSet(newSyncOnly)
	if toClean.Size() > 0 {
		if r.fc != nil {
			close(r.fc.stop)
			select {
			case <-r.fc.stopped:
			case <-time.After(60 * time.Second):
			}
		}
		r.fc = &finalizerCleanup{
			ws:      toClean,
			c:       r,
			stop:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		log.Info("starting finalizer cleaning loop", "toclean", toClean.String())
		go r.fc.clean()
	}

	if err := r.watcher.ReplaceWatch(newSyncOnly.Items()); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.Update(context.Background(), instance); err != nil {
		return reconcile.Result{}, err
	}
	r.watched.Replace(newSyncOnly)
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

type finalizerCleanup struct {
	ws      *watchSet
	c       client.Client
	stop    chan struct{}
	stopped chan struct{}
}

func (fc *finalizerCleanup) clean() {
	defer close(fc.stopped)
	cleanLoop := func() (bool, error) {
		for gvk, _ := range fc.ws.Dump() {
			select {
			case <-fc.stop:
				return true, nil
			default:
				log := log.WithValues("gvk", gvk)
				log.Info("cleaning watch finalizer")
				l := &unstructured.UnstructuredList{}
				listGvk := gvk
				listGvk.Kind = listGvk.Kind + "List"
				l.SetGroupVersionKind(listGvk)
				fc.c.List(context.TODO(), nil, l)
				failure := false
				for _, obj := range l.Items {
					if !syncc.HasFinalizer(&obj) {
						continue
					}
					if err := syncc.RemoveFinalizer(fc.c, &obj); err != nil {
						failure = true
						log.Error(err, "could not remove finalizer", "name", obj.GetName(), "namespace", obj.GetNamespace())
					}
				}
				if !failure {
					instance := &configv1alpha1.Config{}
					if err := fc.c.Get(context.Background(), cfgKey, instance); err != nil {
						log.Info("could not retrieve config to report removed finalizer")
					}
					var allFinalizers []configv1alpha1.GVK
					for _, v := range instance.Status.AllFinalizers {
						if configv1alpha1.ToGVK(v) != gvk {
							allFinalizers = append(allFinalizers, v)
						}
					}
					instance.Status.AllFinalizers = allFinalizers
					if err := fc.c.Update(context.Background(), instance); err != nil {
						log.Info("could not record removed finalizer")
					}
					fc.ws.Remove(gvk)
				}
			}
		}
		if fc.ws.Size() == 0 {
			return true, nil
		}
		return false, nil
	}

	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, cleanLoop); err != nil {
		log.Error(err, "max retries for cleanup", "remaining gvks", fc.ws.Dump())
	}
}

func newSet() *watchSet {
	return &watchSet{
		set: make(map[schema.GroupVersionKind]bool),
	}
}

type watchSet struct {
	mux sync.RWMutex
	set map[schema.GroupVersionKind]bool
}

func (w *watchSet) Size() int {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return len(w.set)
}

func (w *watchSet) Items() []schema.GroupVersionKind {
	w.mux.RLock()
	defer w.mux.RUnlock()
	var r []schema.GroupVersionKind
	for k, _ := range w.set {
		r = append(r, k)
	}
	return r
}

func (w *watchSet) String() string {
	gvks := w.Items()
	var strs []string
	for _, gvk := range gvks {
		strs = append(strs, gvk.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(strs, ", "))
}

func (w *watchSet) Add(gvks ...schema.GroupVersionKind) {
	w.mux.Lock()
	defer w.mux.Unlock()
	for _, gvk := range gvks {
		w.set[gvk] = true
	}
}

func (w *watchSet) Remove(gvks ...schema.GroupVersionKind) {
	w.mux.Lock()
	defer w.mux.Unlock()
	for _, gvk := range gvks {
		delete(w.set, gvk)
	}
}

func (w *watchSet) Dump() map[schema.GroupVersionKind]bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	m := make(map[schema.GroupVersionKind]bool, len(w.set))
	for k, v := range w.set {
		m[k] = v
	}
	return m
}

func (w *watchSet) AddSet(other *watchSet) {
	s := other.Dump()
	w.mux.Lock()
	defer w.mux.Unlock()
	for k, _ := range s {
		w.set[k] = true
	}
}

func (w *watchSet) RemoveSet(other *watchSet) {
	s := other.Dump()
	w.mux.Lock()
	defer w.mux.Unlock()
	for k, _ := range s {
		delete(w.set, k)
	}
}

func (w *watchSet) Equals(other *watchSet) bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	other.mux.RLock()
	defer other.mux.RUnlock()
	return reflect.DeepEqual(w.set, other.set)
}

func (w *watchSet) Replace(other *watchSet) {
	w.mux.Lock()
	defer w.mux.Unlock()
	other.mux.RLock()
	defer other.mux.RUnlock()

	newSet := make(map[schema.GroupVersionKind]bool)
	for k, v := range other.set {
		newSet[k] = v
	}
	w.set = newSet
}
