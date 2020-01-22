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

package constrainttemplate

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/opa/ast"
	errorpkg "github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	finalizerName = "constrainttemplate.finalizers.gatekeeper.sh"
	ctrlName      = "constrainttemplate-controller"
)

var log = logf.Log.WithName("controller").WithValues("kind", "ConstraintTemplate")

type Adder struct {
	Opa          *opa.Client
	WatchManager *watch.Manager
}

// Add creates a new ConstraintTemplate Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.Opa, a.WatchManager)
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

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opa *opa.Client, wm *watch.Manager) (reconcile.Reconciler, error) {
	// constraintsCache contains total number of constraints and shared mutex
	constraintsCache := constraint.NewConstraintsCache()

	constraintAdder := constraint.Adder{Opa: opa, ConstraintsCache: constraintsCache}
	w, err := wm.NewRegistrar(
		ctrlName,
		[]watch.AddFunction{constraintAdder.Add})
	if err != nil {
		return nil, err
	}
	r, err := newStatsReporter()
	if err != nil {
		return nil, err
	}
	return &ReconcileConstraintTemplate{
		Client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		opa:     opa,
		watcher: w,
		metrics: r,
	}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to ConstraintTemplate
	err = c.Watch(&source.Kind{Type: &v1beta1.ConstraintTemplate{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileConstraintTemplate{}

// ReconcileConstraintTemplate reconciles a ConstraintTemplate object
type ReconcileConstraintTemplate struct {
	client.Client
	scheme  *runtime.Scheme
	watcher *watch.Registrar
	opa     *opa.Client
	metrics *reporter
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/status,verbs=get;update;patch

// Reconcile reads that state of the cluster for a ConstraintTemplate object and makes changes based on the state read
// and what is in the ConstraintTemplate.Spec
func (r *ReconcileConstraintTemplate) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the ConstraintTemplate instance
	instance := &v1beta1.ConstraintTemplate{}
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

	defer r.metrics.registry.report(r.metrics)

	status := util.GetCTHAStatus(instance)
	status.Errors = nil
	versionless := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(instance, versionless, nil); err != nil {
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		log.Error(err, "conversion error")
		return reconcile.Result{}, err
	}
	crd, err := r.opa.CreateCRD(context.Background(), versionless)
	if err != nil {
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		var createErr *v1beta1.CreateCRDError
		if parseErrs, ok := err.(ast.Errors); ok {
			for i := 0; i < len(parseErrs); i++ {
				createErr = &v1beta1.CreateCRDError{Code: parseErrs[i].Code, Message: parseErrs[i].Message, Location: parseErrs[i].Location.String()}
				status.Errors = append(status.Errors, createErr)
			}
		} else {
			createErr = &v1beta1.CreateCRDError{Code: "create_error", Message: err.Error()}
			status.Errors = append(status.Errors, createErr)
		}

		util.SetCTHAStatus(instance, status)
		if updateErr := r.Update(context.Background(), instance); updateErr != nil {
			log.Error(updateErr, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
		return reconcile.Result{}, nil
	}
	util.SetCTHAStatus(instance, status)

	name := crd.GetName()
	namespace := crd.GetNamespace()
	if instance.GetDeletionTimestamp().IsZero() {
		// Check if the constraint already exists
		found := &apiextensionsv1beta1.CustomResourceDefinition{}
		err = r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			result, err := r.handleCreate(instance, crd)
			if err != nil {
				r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
			}
			if !result.Requeue {
				r.metrics.registry.add(request.NamespacedName, metrics.ActiveStatus)
			}
			return result, err

		} else if err != nil {
			r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
			return reconcile.Result{}, err

		} else {
			unversionedCRD := &apiextensions.CustomResourceDefinition{}
			if err := r.scheme.Convert(found, unversionedCRD, nil); err != nil {
				r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
				log.Error(err, "conversion error")
				return reconcile.Result{}, err
			}
			result, err := r.handleUpdate(instance, crd, unversionedCRD)
			if err != nil {
				r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
			}
			if !result.Requeue {
				r.metrics.registry.add(request.NamespacedName, metrics.ActiveStatus)
			}
			return result, err
		}

	}
	result, err := r.handleDelete(instance, crd)
	if err != nil {
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
	}
	if !result.Requeue {
		r.metrics.registry.remove(request.NamespacedName)
	}
	return result, err
}

func (r *ReconcileConstraintTemplate) handleCreate(
	instance *v1beta1.ConstraintTemplate,
	crd *apiextensions.CustomResourceDefinition) (reconcile.Result, error) {
	name := crd.GetName()
	log := log.WithValues("name", name)
	log.Info("creating constraint")
	if !containsString(finalizerName, instance.GetFinalizers()) {
		instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
		if err := r.Update(context.Background(), instance); err != nil {
			log.Error(err, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
	}
	log.Info("loading code into OPA")
	versionless := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(instance, versionless, nil); err != nil {
		log.Error(err, "conversion error")
		return reconcile.Result{}, err
	}
	beginCompile := time.Now()
	if _, err := r.opa.AddTemplate(context.Background(), versionless); err != nil {
		if err := r.metrics.reportIngestDuration(metrics.ErrorStatus, time.Since(beginCompile)); err != nil {
			log.Error(err, "failed to report constraint template ingestion duration")
		}
		updateErr := &v1beta1.CreateCRDError{Code: "update_error", Message: fmt.Sprintf("Could not update CRD: %s", err)}
		status := util.GetCTHAStatus(instance)
		status.Errors = append(status.Errors, updateErr)
		util.SetCTHAStatus(instance, status)
		if err2 := r.Update(context.Background(), instance); err2 != nil {
			err = errorpkg.Wrap(err, fmt.Sprintf("Could not update status: %s", err2))
		}
		return reconcile.Result{}, err
	}
	if err := r.metrics.reportIngestDuration(metrics.ActiveStatus, time.Since(beginCompile)); err != nil {
		log.Error(err, "failed to report constraint template ingestion duration")
	}
	log.Info("adding to watcher registry")
	if err := r.watcher.AddWatch(makeGvk(instance.Spec.CRD.Spec.Names.Kind)); err != nil {
		return reconcile.Result{}, err
	}
	// To support HA deployments, only one pod should be able to create CRDs
	log.Info("creating constraint CRD")
	crdv1beta1 := &apiextensionsv1beta1.CustomResourceDefinition{}
	if err := r.scheme.Convert(crd, crdv1beta1, nil); err != nil {
		log.Error(err, "conversion error")
		return reconcile.Result{}, err
	}
	if err := r.Create(context.TODO(), crdv1beta1); err != nil {
		status := util.GetCTHAStatus(instance)
		status.Errors = []*v1beta1.CreateCRDError{}
		createErr := &v1beta1.CreateCRDError{Code: "create_error", Message: fmt.Sprintf("Could not create CRD: %s", err)}
		status.Errors = append(status.Errors, createErr)
		util.SetCTHAStatus(instance, status)
		if err2 := r.Update(context.Background(), instance); err2 != nil {
			err = errorpkg.Wrap(err, fmt.Sprintf("Could not update status: %s", err2))
		}
		return reconcile.Result{}, err
	}
	instance.Status.Created = true
	if err := r.Update(context.Background(), instance); err != nil {
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraintTemplate) handleUpdate(
	instance *v1beta1.ConstraintTemplate,
	crd, found *apiextensions.CustomResourceDefinition) (reconcile.Result, error) {
	// TODO: We may want to only check in code if it has changed. This is harder to do than it sounds
	// because even if the hash hasn't changed, OPA may have been restarted and needs code re-loaded
	// anyway. We should see if the OPA server is smart enough to look for changes on its own, otherwise
	// this may be too expensive to do in large clusters
	name := crd.GetName()
	log := log.WithValues("name", instance.GetName(), "crdName", name)
	if !containsString(finalizerName, instance.GetFinalizers()) {
		instance.SetFinalizers(append(instance.GetFinalizers(), finalizerName))
		if err := r.Update(context.Background(), instance); err != nil {
			log.Error(err, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
	}
	log.Info("loading constraint code into OPA")
	versionless := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(instance, versionless, nil); err != nil {
		log.Error(err, "conversion error")
		return reconcile.Result{}, err
	}
	beginCompile := time.Now()
	if _, err := r.opa.AddTemplate(context.Background(), versionless); err != nil {
		if err := r.metrics.reportIngestDuration(metrics.ErrorStatus, time.Since(beginCompile)); err != nil {
			log.Error(err, "failed to report constraint template ingestion duration")
		}
		updateErr := &v1beta1.CreateCRDError{Code: "update_error", Message: fmt.Sprintf("Could not update CRD: %s", err)}
		status := util.GetCTHAStatus(instance)
		status.Errors = append(status.Errors, updateErr)
		util.SetCTHAStatus(instance, status)
		if err2 := r.Update(context.Background(), instance); err2 != nil {
			err = errorpkg.Wrap(err, fmt.Sprintf("Could not update status: %s", err2))
		}
		return reconcile.Result{}, err
	}
	if err := r.metrics.reportIngestDuration(metrics.ActiveStatus, time.Since(beginCompile)); err != nil {
		log.Error(err, "failed to report constraint template ingestion duration")
	}
	log.Info("making sure constraint is in watcher registry")
	if err := r.watcher.AddWatch(makeGvk(instance.Spec.CRD.Spec.Names.Kind)); err != nil {
		log.Error(err, "error adding template to watch registry")
		return reconcile.Result{}, err
	}
	if !reflect.DeepEqual(crd.Spec, found.Spec) {
		log.Info("difference in spec found, updating")
		found.Spec = crd.Spec
		crdv1beta1 := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := r.scheme.Convert(found, crdv1beta1, nil); err != nil {
			log.Error(err, "conversion error")
			return reconcile.Result{}, err
		}
		if err := r.Update(context.Background(), crdv1beta1); err != nil {
			return reconcile.Result{}, err
		}
	}
	if err := r.Update(context.Background(), instance); err != nil {
		log.Error(err, "update error")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraintTemplate) handleDelete(
	instance *v1beta1.ConstraintTemplate,
	crd *apiextensions.CustomResourceDefinition) (reconcile.Result, error) {
	name := crd.GetName()
	namespace := crd.GetNamespace()
	log := log.WithValues("name", instance.GetName(), "crdName", name)
	if containsString(finalizerName, instance.GetFinalizers()) {
		crdv1beta1 := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := r.scheme.Convert(crd, crdv1beta1, nil); err != nil {
			log.Error(err, "conversion error")
			return reconcile.Result{}, err
		}
		if err := r.Delete(context.Background(), crdv1beta1); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		found := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := r.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, found); err == nil {
			log.Info("child constraint CRD has not yet been deleted, waiting")
			// The following allows the controller to recover from a finalizer deadlock that occurs while
			// the controller is offline
			if err := r.watcher.AddWatch(makeGvk(instance.Spec.CRD.Spec.Names.Kind)); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{Requeue: true}, nil
		} else if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		log.Info("removing from watcher registry")
		if err := r.watcher.RemoveWatch(makeGvk(instance.Spec.CRD.Spec.Names.Kind)); err != nil {
			return reconcile.Result{}, err
		}
		versionless := &templates.ConstraintTemplate{}
		if err := r.scheme.Convert(instance, versionless, nil); err != nil {
			log.Error(err, "conversion error")
			return reconcile.Result{}, err
		}
		if _, err := r.opa.RemoveTemplate(context.Background(), versionless); err != nil {
			return reconcile.Result{}, err
		}
		RemoveFinalizer(instance)

		if err := r.Update(context.Background(), instance); err != nil {
			return reconcile.Result{Requeue: true}, nil
		}
	}
	return reconcile.Result{}, nil
}

func RemoveFinalizer(instance *v1beta1.ConstraintTemplate) {
	instance.SetFinalizers(removeString(finalizerName, instance.GetFinalizers()))
}

func makeGvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    kind,
	}
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

// TearDownState removes all finalizers from constraints and
// constraint templates as well as any pod-specific status written
func TearDownState(c client.Client, finished chan struct{}) {
	defer close(finished)
	toList := func(m map[types.NamespacedName]string) []string {
		var out []string
		for k := range m {
			out = append(out, k.Name)
		}
		return out
	}

	templates := &v1beta1.ConstraintTemplateList{}
	names := make(map[types.NamespacedName]string)
	if err := c.List(context.Background(), templates); err != nil {
		log.Error(err, "could not clean all contraint/template state")
		return
	}
	for _, templ := range templates.Items {
		names[types.NamespacedName{Name: templ.GetName()}] = templ.Spec.CRD.Spec.Names.Kind
	}
	log.Info("found constraint templates to scrub", "templates", toList(names))
	cleanLoop := func() (bool, error) {
		log.Info("removing state from constraint templates", "templates", toList(names))
		for nn, kind := range names {
			listKind := kind + "List"
			// TODO these should be constants somewhere
			gvk := schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: listKind}
			objs := &unstructured.UnstructuredList{}
			objs.SetGroupVersionKind(gvk)
			if err := c.List(context.Background(), objs); err != nil {
				// If the kind is not recognized, there is nothing to clean
				if !meta.IsNoMatchError(err) {
					log.Error(err, "while listing constraints for cleanup", "kind", listKind)
					continue
				}
			}
			success := true
			for _, obj := range objs.Items {
				log.Info("scrubing constraint state", "name", obj.GetName())
				constraint.RemoveFinalizer(&obj)
				if err := util.DeleteHAStatus(&obj); err != nil {
					success = false
					log.Error(err, "could not remove pod-specific status")
				}
				if err := c.Update(context.Background(), &obj); err != nil {
					success = false
					log.Error(err, "could not scrub constraint state", "name", obj.GetName())
				}
			}
			if success {
				templ := &v1beta1.ConstraintTemplate{}
				if err := c.Get(context.Background(), nn, templ); err != nil {
					if errors.IsNotFound(err) {
						delete(names, nn)
						continue
					} else {
						log.Error(err, "while retrieving constraint template for cleanup", "template", nn)
						continue
					}
				}
				RemoveFinalizer(templ)
				util.DeleteCTHAStatus(templ)
				if err := c.Update(context.Background(), templ); err != nil && !errors.IsNotFound(err) {
					log.Error(err, "while writing a constraint template for cleanup", "template", nn)
					continue
				}
				delete(names, nn)
			}
		}
		if len(names) == 0 {
			return true, nil
		}
		return false, nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 50 * time.Millisecond,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, cleanLoop); err != nil {
		log.Error(err, "max retries for cleanup", "remaining constraint kinds", names)
	}
}
