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
	"reflect"

	templatesv1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

const finalizerName = "mutationtemplate.finalizers.gatekeeper.sh"

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
		log.Error(err, "error retrieving an MutationTemplate instance")
		return reconcile.Result{}, err
	}
	log.Info("Reconciling", "MutationTemplate", instance)

	crd, err := r.opa.CreateMutationCRD(context.Background(), instance)
	if err != nil {
		log.Error(err, "CreateMutationCRD error")
		return reconcile.Result{}, err
	}

	// If the MutationTemplate is not currently being deleted,
	// check to see if the corresponding Mutation CRD exists.
	// If it doesn't, create it. If it does exist, check to see if it has changed (warranting an update)
	if instance.GetDeletionTimestamp().IsZero() {
		// Check if the mutation already exists
		found := &apiextensionsv1beta1.CustomResourceDefinition{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: crd.GetName()}, found)
		if err != nil && errors.IsNotFound(err) {
			return r.handleCreate(instance, crd)
		} else if err != nil {
			log.Error(err, "error retrieving an Mutation CRD instance")
			return reconcile.Result{}, err
		}
		return r.handleUpdate(instance, crd, found)
	}

	return r.handleDelete(instance, crd)
}

func (r *ReconcileMutationTemplate) handleCreate(instance *templatesv1alpha1.MutationTemplate, crd *apiextensionsv1beta1.CustomResourceDefinition) (reconcile.Result, error) {
	log := log.WithValues("name", crd.GetName())
	log.Info("creating mutation")
	if !containsString(finalizerName, instance.GetFinalizers()) {
		instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
		if err := r.Update(context.Background(), instance); err != nil {
			log.Error(err, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
	}
	log.Info("creating mutation CRD")
	if err := r.Create(context.TODO(), crd); err != nil {
		log.Error(err, "CRD creation error")
		return reconcile.Result{}, err
	}
	instance.Status.Created = true
	if err := r.Update(context.Background(), instance); err != nil {
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileMutationTemplate) handleUpdate(
	instance *templatesv1alpha1.MutationTemplate,
	crd, found *apiextensionsv1beta1.CustomResourceDefinition) (reconcile.Result, error) {
	log := log.WithValues("name", instance.GetName(), "crdName", crd.GetName())
	if !reflect.DeepEqual(crd.Spec, found.Spec) {
		log.Info("difference in spec found, updating")
		found.Spec = crd.Spec
		if err := r.Update(context.Background(), found); err != nil {
			log.Error(err, "update error")
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileMutationTemplate) handleDelete(instance *templatesv1alpha1.MutationTemplate, crd *apiextensionsv1beta1.CustomResourceDefinition) (reconcile.Result, error) {
	log := log.WithValues("name", instance.GetName(), "crdName", crd.GetName())
	if containsString(finalizerName, instance.GetFinalizers()) {
		if err := r.Delete(context.Background(), crd); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "deletion error")
			return reconcile.Result{}, err
		}
		found := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := r.Get(context.Background(), types.NamespacedName{Name: crd.GetName()}, found); err == nil {
			log.Info("mutation CRD has not yet been deleted, waiting")
			return reconcile.Result{Requeue: true}, nil
		} else if err != nil && !errors.IsNotFound(err) {
			log.Error(err, "error finding the Mutation CRD")
			return reconcile.Result{}, err
		}

		instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
		if err := r.Update(context.Background(), instance); err != nil {
			return reconcile.Result{Requeue: true}, nil
		}
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
