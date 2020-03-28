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
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	constraintutil "github.com/open-policy-agent/gatekeeper/pkg/util/constraint"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/opa/ast"
	errorpkg "github.com/pkg/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
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

var log = logf.Log.WithName("controller").WithValues("kind", "ConstraintTemplate", logging.Process, "constraint_template_controller")

var gvkConstraintTemplate = schema.GroupVersionKind{
	Group:   v1beta1.SchemeGroupVersion.Group,
	Version: v1beta1.SchemeGroupVersion.Version,
	Kind:    "ConstraintTemplate",
}

type Adder struct {
	Opa              *opa.Client
	WatchManager     *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
}

// Add creates a new ConstraintTemplate Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr, a.Opa, a.WatchManager, a.ControllerSwitch, a.Tracker)
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

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opa *opa.Client, wm *watch.Manager, cs *watch.ControllerSwitch, tracker *readiness.Tracker) (reconcile.Reconciler, error) {
	// constraintsCache contains total number of constraints and shared mutex
	constraintsCache := constraint.NewConstraintsCache()

	// Events will be used to receive events from dynamic watches registered
	// via the registrar below.
	events := make(chan event.GenericEvent, 1024)
	constraintAdder := constraint.Adder{
		Opa:              opa,
		ConstraintsCache: constraintsCache,
		WatchManager:     wm,
		ControllerSwitch: cs,
		Events:           events,
		Tracker:          tracker,
	}
	// Create subordinate controller - we will feed it events dynamically via watch
	if err := constraintAdder.Add(mgr); err != nil {
		return nil, fmt.Errorf("registering constraint controller: %w", err)
	}

	w, err := wm.NewRegistrar(ctrlName, events)
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
		cs:      cs,
		metrics: r,
		tracker: tracker,
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

	// Watch for changes to Constraint CRDs
	err = c.Watch(
		&source.Kind{Type: &apiextensionsv1beta1.CustomResourceDefinition{}},
		&handler.EnqueueRequestForOwner{
			OwnerType:    &v1beta1.ConstraintTemplate{},
			IsController: true,
		},
	)
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
	cs      *watch.ControllerSwitch
	metrics *reporter
	tracker *readiness.Tracker
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/status,verbs=get;update;patch

// Reconcile reads that state of the cluster for a ConstraintTemplate object and makes changes based on the state read
// and what is in the ConstraintTemplate.Spec
func (r *ReconcileConstraintTemplate) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log := log.WithValues("template_name", request.Name)
	// Short-circuit if shutting down.
	if r.cs != nil {
		running := r.cs.Enter()
		defer r.cs.Exit()
		if !running {
			return reconcile.Result{}, nil
		}
	}

	defer r.metrics.registry.report(r.metrics)

	// Fetch the ConstraintTemplate instance
	deleted := false
	ct := &v1beta1.ConstraintTemplate{}
	err := r.Get(context.TODO(), request.NamespacedName, ct)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
		// be sure we are using a blank constraint template so that
		// we know finalizer removal code won't break (can be removed once that
		// code is removed)
		ct = &v1beta1.ConstraintTemplate{}
	}
	deleted = deleted || !ct.GetDeletionTimestamp().IsZero()

	if containsString(finalizerName, ct.GetFinalizers()) {
		// preserve original status as otherwise it will get wiped in the update
		origStatus := ct.Status.DeepCopy()
		RemoveFinalizer(ct)
		if err := r.Update(context.Background(), ct); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
		ct.Status = *origStatus
	}

	if deleted {
		ctRef := &templates.ConstraintTemplate{}
		ctRef.SetNamespace(request.Namespace)
		ctRef.SetName(request.Name)
		ctUnversioned, err := r.opa.GetTemplate(context.TODO(), ctRef)
		if err != nil {
			log.Info("missing constraint template in OPA cache, no deletion necessary")
			logAction(ctRef, deletedAction)
			r.metrics.registry.remove(request.NamespacedName)
			return reconcile.Result{}, nil
		}
		result, err := r.handleDelete(ctUnversioned)
		if err != nil {
			logError(request.NamespacedName.Name)
			r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		} else if !result.Requeue {
			logAction(ct, deletedAction)
			r.metrics.registry.remove(request.NamespacedName)
		}
		return result, err
	}

	status := util.GetCTHAStatus(ct)
	status.Errors = nil
	unversionedCT := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(ct, unversionedCT, nil); err != nil {
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		log.Error(err, "conversion error")
		logError(request.NamespacedName.Name)
		return reconcile.Result{}, err
	}
	unversionedProposedCRD, err := r.opa.CreateCRD(context.Background(), unversionedCT)
	if err != nil {
		r.tracker.CancelTemplate(unversionedCT) // Don't track templates that failed compilation
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

		util.SetCTHAStatus(ct, status)
		if updateErr := r.Status().Update(context.Background(), ct); updateErr != nil {
			log.Error(updateErr, "update error")
			return reconcile.Result{Requeue: true}, nil
		}
		logError(request.NamespacedName.Name)
		return reconcile.Result{}, nil
	}
	util.SetCTHAStatus(ct, status)

	proposedCRD := &apiextensionsv1beta1.CustomResourceDefinition{}
	if err := r.scheme.Convert(unversionedProposedCRD, proposedCRD, nil); err != nil {
		r.tracker.CancelTemplate(unversionedCT) // Don't track templates that failed compilation
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		log.Error(err, "conversion error")
		logError(request.NamespacedName.Name)
		err := r.reportErrorOnCTStatus("conversion_error", "Could not convert from unversioned resource", ct, err)
		return reconcile.Result{}, err
	}

	name := unversionedProposedCRD.GetName()
	namespace := unversionedProposedCRD.GetNamespace()
	// Check if the constraint CRD already exists
	action := updatedAction
	currentCRD := &apiextensionsv1beta1.CustomResourceDefinition{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, currentCRD)
	switch {
	case err == nil:
		break

	case errors.IsNotFound(err):
		action = createdAction
		currentCRD = nil

	default:
		logError(request.NamespacedName.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		return reconcile.Result{}, err
	}

	result, err := r.handleUpdate(ct, unversionedCT, proposedCRD, currentCRD)
	if err != nil {
		logError(request.NamespacedName.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
	} else if !result.Requeue {
		logAction(ct, action)
		r.metrics.registry.add(request.NamespacedName, metrics.ActiveStatus)
	}
	return result, err
}

func (r *ReconcileConstraintTemplate) reportErrorOnCTStatus(code, message string, ct *v1beta1.ConstraintTemplate, err error) error {
	status := util.GetCTHAStatus(ct)
	status.Errors = []*v1beta1.CreateCRDError{}
	createErr := &v1beta1.CreateCRDError{
		Code:    code,
		Message: fmt.Sprintf("%s: %s", message, err),
	}
	status.Errors = append(status.Errors, createErr)
	util.SetCTHAStatus(ct, status)
	if err2 := r.Status().Update(context.Background(), ct); err2 != nil {
		return errorpkg.Wrap(err, fmt.Sprintf("Could not update status: %s", err2))
	}
	return err
}

func (r *ReconcileConstraintTemplate) handleUpdate(
	ct *v1beta1.ConstraintTemplate,
	unversionedCT *templates.ConstraintTemplate,
	proposedCRD, currentCRD *apiextensionsv1beta1.CustomResourceDefinition) (reconcile.Result, error) {
	name := proposedCRD.GetName()
	log := log.WithValues("name", ct.GetName(), "crdName", name)

	log.Info("loading code into OPA")
	beginCompile := time.Now()

	// It's important that opa.AddTemplate() is called first. That way we can
	// rely on a template's existence in OPA to know whether a watch needs
	// to be removed
	if _, err := r.opa.AddTemplate(context.Background(), unversionedCT); err != nil {
		if err := r.metrics.reportIngestDuration(metrics.ErrorStatus, time.Since(beginCompile)); err != nil {
			log.Error(err, "failed to report constraint template ingestion duration")
		}
		err := r.reportErrorOnCTStatus("ingest_error", "Could not ingest Rego", ct, err)
		r.tracker.CancelTemplate(unversionedCT) // Don't track templates that failed compilation
		return reconcile.Result{}, err
	}

	if err := r.metrics.reportIngestDuration(metrics.ActiveStatus, time.Since(beginCompile)); err != nil {
		log.Error(err, "failed to report constraint template ingestion duration")
	}

	// Mark for readiness tracking
	t := r.tracker.For(gvkConstraintTemplate)
	t.Observe(unversionedCT)
	log.Info("[readiness] observed ConstraintTemplate", "name", unversionedCT.GetName())

	var newCRD *apiextensionsv1beta1.CustomResourceDefinition
	if currentCRD == nil {
		newCRD = proposedCRD.DeepCopy()
	} else {
		newCRD = currentCRD.DeepCopy()
		newCRD.Spec = proposedCRD.Spec
	}

	if err := controllerutil.SetControllerReference(ct, newCRD, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	if currentCRD == nil {
		log.Info("creating crd")
		if err := r.Create(context.TODO(), newCRD); err != nil {
			err := r.reportErrorOnCTStatus("create_error", "Could not create CRD", ct, err)
			return reconcile.Result{}, err
		}
	} else if !reflect.DeepEqual(newCRD, currentCRD) {
		log.Info("updating crd")
		if err := r.Update(context.Background(), newCRD); err != nil {
			err := r.reportErrorOnCTStatus("update_error", "Could not update CRD", ct, err)
			return reconcile.Result{}, err
		}
	}
	// This must go after CRD creation/update as otherwise AddWatch will always fail
	log.Info("making sure constraint is in watcher registry")
	if err := r.watcher.AddWatch(makeGvk(ct.Spec.CRD.Spec.Names.Kind)); err != nil {
		log.Error(err, "error adding template to watch registry")
		return reconcile.Result{}, err
	}
	ct.Status.Created = true
	if err := r.Status().Update(context.Background(), ct); err != nil {
		log.Error(err, "update error")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraintTemplate) handleDelete(
	ct *templates.ConstraintTemplate) (reconcile.Result, error) {
	log := log.WithValues("name", ct.GetName())
	log.Info("removing from watcher registry")
	gvk := makeGvk(ct.Spec.CRD.Spec.Names.Kind)
	if err := r.watcher.RemoveWatch(gvk); err != nil {
		return reconcile.Result{}, err
	}
	r.tracker.CancelTemplate(ct)

	// removing the template from the OPA cache must go last as we are relying
	// on that cache to derive the Kind to remove from the watch
	if _, err := r.opa.RemoveTemplate(context.Background(), ct); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

type action string

const (
	createdAction = action("created")
	updatedAction = action("updated")
	deletedAction = action("deleted")
)

type namedObj interface {
	GetName() string
}

func logAction(template namedObj, a action) {
	log.Info(
		fmt.Sprintf("template was %s", string(a)),
		logging.EventType, fmt.Sprintf("template_%s", string(a)),
		logging.TemplateName, template.GetName(),
	)
}

func logError(name string) {
	log.Info(
		"unable to ingest template",
		logging.EventType, "template_ingest_error",
		logging.TemplateName, name,
	)
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
				log.Info("scrubbing constraint state", "name", obj.GetName())
				constraint.RemoveFinalizer(&obj)
				if err := c.Update(context.Background(), &obj); err != nil {
					success = false
					log.Error(err, "could not scrub constraint finalizer", "name", obj.GetName())
				}

				if err := constraintutil.DeleteHAStatus(&obj); err != nil {
					success = false
					log.Error(err, "could not remove pod-specific status")
				}
				if err := c.Status().Update(context.Background(), &obj); err != nil {
					success = false
					log.Error(err, "could not scrub constraint status", "name", obj.GetName())
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
				if err := c.Update(context.Background(), templ); err != nil && !errors.IsNotFound(err) {
					log.Error(err, "while writing a constraint template for finalizer cleanup", "template", nn)
					continue
				}
				util.DeleteCTHAStatus(templ)
				if err := c.Status().Update(context.Background(), templ); err != nil && !errors.IsNotFound(err) {
					log.Error(err, "while writing a constraint template for status cleanup", "template", nn)
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
