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

package constraint

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
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

var (
	log                   = logf.Log.WithName("controller").WithValues("metaKind", "Constraint")
	knownConstraintStatus = []status{activeStatus, errorStatus}
)

const (
	finalizerName        = "finalizers.gatekeeper.sh/constraint"
	activeStatus  status = "active"
	errorStatus   status = "error"
)

type Adder struct {
	Opa              *opa.Client
	ConstraintsCache *ConstraintsCache
}

type ConstraintsCache struct {
	mux   sync.RWMutex
	cache map[string]tags
}

type tags struct {
	enforcementAction util.EnforcementAction
	status            status
}

type status string

// Add creates a new Constraint Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager, gvk schema.GroupVersionKind) error {
	reporter, err := newStatsReporter()
	if err != nil {
		log.Error(err, "StatsReporter could not start")
		return err
	}

	r := newReconciler(mgr, gvk, a.Opa, reporter, a.ConstraintsCache)
	return add(mgr, r, gvk)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, gvk schema.GroupVersionKind, opa *opa.Client, reporter StatsReporter, constraintsCache *ConstraintsCache) reconcile.Reconciler {
	return &ReconcileConstraint{
		Client:           mgr.GetClient(),
		scheme:           mgr.GetScheme(),
		opa:              opa,
		log:              log.WithValues("kind", gvk.Kind, "apiVersion", gvk.GroupVersion().String()),
		gvk:              gvk,
		reporter:         reporter,
		constraintsCache: constraintsCache,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, gvk schema.GroupVersionKind) error {
	// Create a new controller
	c, err := controller.New(fmt.Sprintf("%s-constraint-controller", gvk.String()), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided constraint
	instance := unstructured.Unstructured{}
	instance.SetGroupVersionKind(gvk)
	err = c.Watch(&source.Kind{Type: &instance}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileConstraint{}

// ReconcileSync reconciles an arbitrary constraint object described by Kind
type ReconcileConstraint struct {
	client.Client
	scheme           *runtime.Scheme
	opa              *opa.Client
	gvk              schema.GroupVersionKind
	log              logr.Logger
	reporter         StatsReporter
	constraintsCache *ConstraintsCache
}

// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a constraint object and makes changes based on the state read
// and what is in the constraint.Spec
func (r *ReconcileConstraint) Reconcile(request reconcile.Request) (reconcile.Result, error) {
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

	constraintKey := strings.Join([]string{instance.GetKind(), instance.GetName()}, "/")
	enforcementAction, err := util.GetEnforcementAction(instance.Object)
	if err != nil {
		return reconcile.Result{}, err
	}

	reportMetrics := false
	defer func() {
		if reportMetrics {
			r.constraintsCache.reportTotalConstraints(r.reporter)
		}
	}()

	if instance.GetDeletionTimestamp().IsZero() {
		if !HasFinalizer(instance) {
			instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
		log.Info("instance will be added", "instance", instance)
		status, err := util.GetHAStatus(instance)
		if err != nil {
			return reconcile.Result{}, err
		}
		delete(status, "errors")
		if err = util.SetHAStatus(instance, status); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.cacheConstraint(instance); err != nil {
			r.constraintsCache.addConstraintKey(constraintKey, tags{
				enforcementAction: enforcementAction,
				status:            errorStatus,
			})
			reportMetrics = true
			return reconcile.Result{}, err
		}
		status, err = util.GetHAStatus(instance)
		if err != nil {
			return reconcile.Result{}, err
		}
		status["enforced"] = true
		if err = util.SetHAStatus(instance, status); err != nil {
			return reconcile.Result{}, err
		}
		if err = r.Update(context.Background(), instance); err != nil {
			return reconcile.Result{Requeue: true}, nil
		}
		// adding constraint to cache and sending metrics
		r.constraintsCache.addConstraintKey(constraintKey, tags{
			enforcementAction: enforcementAction,
			status:            activeStatus,
		})
		reportMetrics = true
	} else {
		// Handle deletion
		if HasFinalizer(instance) {
			if _, err := r.opa.RemoveConstraint(context.Background(), instance); err != nil {
				if _, ok := err.(*opa.UnrecognizedConstraintError); !ok {
					return reconcile.Result{}, err
				}
			}
			RemoveFinalizer(instance)
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
			// removing constraint entry from cache
			r.constraintsCache.deleteConstraintKey(constraintKey)
			reportMetrics = true
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraint) cacheConstraint(instance *unstructured.Unstructured) error {
	obj := instance.DeepCopy()
	// Remove the status field since we do not need it for OPA
	unstructured.RemoveNestedField(obj.Object, "status")
	_, err := r.opa.AddConstraint(context.Background(), obj)
	return err
}

func RemoveFinalizer(instance *unstructured.Unstructured) {
	instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
}

func HasFinalizer(instance *unstructured.Unstructured) bool {
	return containsString(finalizerName, instance.GetFinalizers())
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

func NewConstraintsCache() *ConstraintsCache {
	return &ConstraintsCache{
		cache: make(map[string]tags),
	}
}

func (c *ConstraintsCache) addConstraintKey(constraintKey string, t tags) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.cache[constraintKey] = tags{
		enforcementAction: t.enforcementAction,
		status:            t.status,
	}
}

func (c *ConstraintsCache) deleteConstraintKey(constraintKey string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.cache, constraintKey)
}

func (c *ConstraintsCache) reportTotalConstraints(reporter StatsReporter) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	totals := make(map[tags]int)
	// report total number of constraints
	for _, v := range c.cache {
		totals[v]++
	}

	for _, enforcementAction := range util.KnownEnforcementActions {
		for _, status := range knownConstraintStatus {
			if err := reporter.reportConstraints(
				tags{
					enforcementAction: enforcementAction,
					status:            status,
				},
				int64(totals[tags{
					enforcementAction: enforcementAction,
					status:            status,
				}])); err != nil {
				log.Error(err, "failed to report total constraints")
			}
		}
	}
}
