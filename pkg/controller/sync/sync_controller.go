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

package sync

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

var log = logf.Log.WithName("controller").WithValues("metaKind", "Sync")

type Adder struct {
	Opa *opa.Client
}

// Add creates a new Sync Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager, gvk schema.GroupVersionKind, cs *watch.ControllerSwitch) error {
	r := newReconciler(mgr, gvk, a.Opa, cs)
	return add(mgr, r, gvk)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, gvk schema.GroupVersionKind, opa *opa.Client, cs *watch.ControllerSwitch) reconcile.Reconciler {
	return &ReconcileSync{
		Client: mgr.GetClient(),
		cs:     cs,
		scheme: mgr.GetScheme(),
		opa:    opa,
		log:    log.WithValues("kind", gvk.Kind, "apiVersion", gvk.GroupVersion().String()),
		gvk:    gvk,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, gvk schema.GroupVersionKind) error {
	// Create a new controller
	c, err := controller.New(fmt.Sprintf("%s-sync-controller", gvk.String()), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided resource
	instance := unstructured.Unstructured{}
	instance.SetGroupVersionKind(gvk)
	err = c.Watch(&source.Kind{Type: &instance}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileSync{}

// ReconcileSync reconciles an arbitrary object described by Kind
type ReconcileSync struct {
	client.Client
	cs     *watch.ControllerSwitch
	scheme *runtime.Scheme
	opa    *opa.Client
	gvk    schema.GroupVersionKind
	log    logr.Logger
}

// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for an object and makes changes based on the state read
// and what is in the constraint.Spec
func (r *ReconcileSync) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	enabled := r.cs.Enter()
	defer r.cs.Exit()
	if !enabled {
		r.log.Info("ignoring request, sync controller disabled", "request", request)
		return reconcile.Result{}, nil
	}
	instance := &unstructured.Unstructured{}
	instance.SetGroupVersionKind(r.gvk)
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// This is a deletion; remove the data
			instance.SetNamespace(request.Namespace)
			instance.SetName(request.Name)
			if _, err := r.opa.RemoveData(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	// For some reason 'Status' objects corresponding to rejection messages are being pushed
	if instance.GroupVersionKind() != r.gvk {
		r.log.Info("ignoring unexpected data", "data", instance)
		return reconcile.Result{}, nil
	}

	if !instance.GetDeletionTimestamp().IsZero() {
		if _, err := r.opa.RemoveData(context.Background(), instance); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	r.log.Info("data will be added", "data", instance)
	if _, err := r.opa.AddData(context.Background(), instance); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
