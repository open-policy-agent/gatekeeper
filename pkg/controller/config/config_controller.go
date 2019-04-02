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
	"reflect"
	"sync"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	syncv1alpha1 "github.com/open-policy-agent/gatekeeper/pkg/apis/sync/v1alpha1"
	syncc "github.com/open-policy-agent/gatekeeper/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const ctrlName = "config-controller"

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
	err = c.Watch(&source.Kind{Type: &syncv1alpha1.Config{}}, &handler.EnqueueRequestForObject{})
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
	instance := &syncv1alpha1.Config{}
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

	s := newSet()
	var gvks []schema.GroupVersionKind
	for _, entry := range instance.Spec.Whitelist {
		gvk := schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind}
		gvks = append(gvks, gvk)
		s.Add(gvk)
	}
	if !r.watched.Equals(s) {
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
	if err := r.watcher.ReplaceWatch(gvks); err != nil {
		return reconcile.Result{}, err
	}

	// TODO report status of config
	// TODO enforce singleton by forcing name to be "config"
	return reconcile.Result{}, nil
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

func (w *watchSet) Add(gvks ...schema.GroupVersionKind) {
	w.mux.Lock()
	defer w.mux.Unlock()
	for _, gvk := range gvks {
		w.set[gvk] = true
	}
}

func (w *watchSet) Equals(other *watchSet) bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	other.mux.RLock()
	defer other.mux.RUnlock()
	return reflect.DeepEqual(w, other)
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
