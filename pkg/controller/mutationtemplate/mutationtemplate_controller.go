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

package mutationtemplate

import (
	"context"
	"fmt"

	templatesv1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

var log = logf.Log.WithName("controller").WithValues("kind", "MutationTemplate")

type Adder struct {
	Opa          opa.Client
	WatchManager *watch.WatchManager
}

// Add creates a new MutationTemplate Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.Opa)
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
func newReconciler(mgr manager.Manager, opa opa.Client) (reconcile.Reconciler, error) {
	return &ReconcileMutationTemplate{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		opa:    opa,
	}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("mutationtemplate-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to MutationTemplate
	err = c.Watch(&source.Kind{Type: &templatesv1alpha1.MutationTemplate{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create
	// Uncomment watch a Deployment created by MutationTemplate - change this for objects you create
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &templatesv1alpha1.MutationTemplate{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileMutationTemplate{}

// ReconcileMutationTemplate reconciles a MutationTemplate object
type ReconcileMutationTemplate struct {
	client.Client
	scheme *runtime.Scheme
	opa    opa.Client
}

// Reconcile reads that state of the cluster for a MutationTemplate object and makes changes based on the state read
// and what is in the MutationTemplate.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  The scaffolding writes
// a Deployment as an example
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=mutationtemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=mutationtemplates/status,verbs=get;update;patch
func (r *ReconcileMutationTemplate) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the MutationTemplate instance
	instance := &templatesv1alpha1.MutationTemplate{}
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
	log.Info("Reconciling", "MutationTemplate", instance)
	log.Info("There is also an", "opa.Client", fmt.Sprintf("%p", r.opa))

	return reconcile.Result{}, nil
}
