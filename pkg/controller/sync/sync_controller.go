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
	"reflect"

	"github.com/go-logr/logr"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
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

const (
	finalizerName = "finalizers.gatekeeper.sh/sync"
)

type Adder struct {
	Opa *opa.Client
}

// Add creates a new Sync Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager, gvk schema.GroupVersionKind) error {
	r := newReconciler(mgr, gvk, a.Opa)
	return add(mgr, r, gvk)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, gvk schema.GroupVersionKind, opa *opa.Client) reconcile.Reconciler {
	return &ReconcileSync{
		Client: mgr.GetClient(),
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
	scheme *runtime.Scheme
	opa    *opa.Client
	gvk    schema.GroupVersionKind
	log    logr.Logger
}

// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for an object and makes changes based on the state read
// and what is in the constraint.Spec
func (r *ReconcileSync) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	instance := &unstructured.Unstructured{}
	instance.SetGroupVersionKind(r.gvk)
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
	// For some reason 'Status' objects corresponding to rejection messages are being pushed
	if instance.GroupVersionKind() != r.gvk {
		log.Info("ignoring unexpected data", "data", instance)
		return reconcile.Result{}, nil
	}

	if instance.GetDeletionTimestamp().IsZero() {
		if !containsString(finalizerName, instance.GetFinalizers()) {
			instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
			// For some reason the instance sometimes gets changed by update when there is a race
			// condition that leads to a validating webhook deny of the update
			cpy := instance.DeepCopy()
			if err := r.Update(context.Background(), cpy); err != nil {
				return reconcile.Result{}, err
			}
			if !reflect.DeepEqual(instance, cpy) {
				log.Info("instance and cpy differ")
			}
		}
		log.Info("data will be added", "data", instance)
		if _, err := r.opa.AddData(context.Background(), instance); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deletion
		if HasFinalizer(instance) {
			if _, err := r.opa.RemoveData(context.Background(), instance); err != nil {
				return reconcile.Result{}, err
			}
			if err := RemoveFinalizer(r, instance); err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func HasFinalizer(obj *unstructured.Unstructured) bool {
	return containsString(finalizerName, obj.GetFinalizers())
}

func RemoveFinalizer(c client.Client, obj *unstructured.Unstructured) error {
	obj.SetFinalizers(removeString(finalizerName, obj.GetFinalizers()))
	return c.Update(context.Background(), obj)
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
