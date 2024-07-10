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
	"errors"
	"flag"
	"fmt"
	"reflect"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	pSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/transform"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraintstatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constrainttemplatestatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	errorpkg "github.com/pkg/errors"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ctrlName = "constrainttemplate-controller"
)

var (
	logger             = log.Log.V(logging.DebugLevel).WithName("controller").WithValues("kind", "ConstraintTemplate", logging.Process, "constraint_template_controller")
	discoveryErr       *apiutil.ErrResourceDiscoveryFailed
	defaultGenerateVAP = flag.Bool("default-create-vap-for-templates", false, "Create VAP resource for template containing CEL source. Allowed values are false: do not create Validating Admission Policy unless generateVAP: true is set on constraint template explicitly, true: create Validating Admissoin Policy unless generateVAP: false is set on constraint template explicitly.")
)

var gvkConstraintTemplate = schema.GroupVersionKind{
	Group:   v1beta1.SchemeGroupVersion.Group,
	Version: v1beta1.SchemeGroupVersion.Version,
	Kind:    "ConstraintTemplate",
}

type Adder struct {
	CFClient         *constraintclient.Client
	WatchManager     *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	GetPod           func(context.Context) (*corev1.Pod, error)
}

// Add creates a new ConstraintTemplate Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.HasValidationOperations() {
		return nil
	}
	// events will be used to receive events from dynamic watches registered
	events := make(chan event.GenericEvent, 1024)
	r, err := newReconciler(mgr, a.CFClient, a.WatchManager, a.ControllerSwitch, a.Tracker, events, events, a.GetPod)
	if err != nil {
		return err
	}
	return add(mgr, r)
}

func (a *Adder) InjectCFClient(c *constraintclient.Client) {
	a.CFClient = c
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

func (a *Adder) InjectGetPod(getPod func(context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

// newReconciler returns a new reconcile.Reconciler
// cstrEvents is the channel from which constraint controller will receive the events
// regEvents is the channel registered by Registrar to put the events in
// cstrEvents and regEvents point to same event channel except for testing.
func newReconciler(mgr manager.Manager, cfClient *constraintclient.Client, wm *watch.Manager, cs *watch.ControllerSwitch, tracker *readiness.Tracker, cstrEvents <-chan event.GenericEvent, regEvents chan<- event.GenericEvent, getPod func(context.Context) (*corev1.Pod, error)) (*ReconcileConstraintTemplate, error) {
	// constraintsCache contains total number of constraints and shared mutex and vap label
	constraintsCache := constraint.NewConstraintsCache()

	w, err := wm.NewRegistrar(ctrlName, regEvents)
	if err != nil {
		return nil, err
	}
	statusW, err := wm.NewRegistrar(ctrlName+"-status", regEvents)
	if err != nil {
		return nil, err
	}

	// via the registrar below.

	constraintAdder := constraint.Adder{
		CFClient:         cfClient,
		ConstraintsCache: constraintsCache,
		WatchManager:     wm,
		ControllerSwitch: cs,
		Events:           cstrEvents,
		Tracker:          tracker,
		GetPod:           getPod,
		IfWatching:       w.IfWatching,
	}
	// Create subordinate controller - we will feed it events dynamically via watch
	if err := constraintAdder.Add(mgr); err != nil {
		return nil, fmt.Errorf("registering constraint controller: %w", err)
	}

	if operations.IsAssigned(operations.Status) {
		// statusEvents will be used to receive events from dynamic watches registered
		// via the registrar below.
		statusEvents := make(chan event.GenericEvent, 1024)
		csAdder := constraintstatus.Adder{
			CFClient:         cfClient,
			WatchManager:     wm,
			ControllerSwitch: cs,
			Events:           statusEvents,
			IfWatching:       statusW.IfWatching,
		}
		if err := csAdder.Add(mgr); err != nil {
			return nil, err
		}

		ctsAdder := constrainttemplatestatus.Adder{
			CfClient:         cfClient,
			WatchManager:     wm,
			ControllerSwitch: cs,
		}
		if err := ctsAdder.Add(mgr); err != nil {
			return nil, err
		}
	}

	r := newStatsReporter()
	reconciler := &ReconcileConstraintTemplate{
		Client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		cfClient:      cfClient,
		watcher:       w,
		statusWatcher: statusW,
		cs:            cs,
		metrics:       r,
		tracker:       tracker,
		getPod:        getPod,
		cstrEvents:    regEvents,
	}

	if getPod == nil {
		reconciler.getPod = reconciler.defaultGetPod
	}
	return reconciler, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to ConstraintTemplate
	err = c.Watch(source.Kind(mgr.GetCache(), &v1beta1.ConstraintTemplate{}), &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to ConstraintTemplateStatus
	err = c.Watch(
		source.Kind(mgr.GetCache(), &statusv1beta1.ConstraintTemplatePodStatus{}),
		handler.EnqueueRequestsFromMapFunc(constrainttemplatestatus.PodStatusToConstraintTemplateMapper(true)),
	)
	if err != nil {
		return err
	}

	// Watch for changes to Constraint CRDs
	err = c.Watch(
		source.Kind(mgr.GetCache(), &apiextensionsv1.CustomResourceDefinition{}),
		handler.EnqueueRequestForOwner(
			mgr.GetScheme(),
			mgr.GetRESTMapper(),
			&v1beta1.ConstraintTemplate{},
			handler.OnlyControllerOwner(),
		),
	)
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileConstraintTemplate{}

// ReconcileConstraintTemplate reconciles a ConstraintTemplate object.
type ReconcileConstraintTemplate struct {
	client.Client
	scheme        *runtime.Scheme
	watcher       *watch.Registrar
	statusWatcher *watch.Registrar
	cfClient      *constraintclient.Client
	cs            *watch.ControllerSwitch
	metrics       *reporter
	tracker       *readiness.Tracker
	getPod        func(context.Context) (*corev1.Pod, error)
	cstrEvents    chan<- event.GenericEvent
}

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingadmissionpolicies;validatingadmissionpolicybindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch;create;update;patch;delete
// TODO(acpana): remove in 3.16 as per https://github.com/open-policy-agent/gatekeeper/issues/3084
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/finalizers,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=externaldata.gatekeeper.sh,resources=providers,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a ConstraintTemplate object and makes changes based on the state read
// and what is in the ConstraintTemplate.Spec.
func (r *ReconcileConstraintTemplate) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := logger.WithValues("template_name", request.Name)
	// Short-circuit if shutting down.
	if r.cs != nil {
		running := r.cs.Enter()
		defer r.cs.Exit()
		if !running {
			return reconcile.Result{}, nil
		}
	}

	defer r.metrics.registry.report(ctx, r.metrics)

	// Fetch the ConstraintTemplate instance
	deleted := false
	ct := &v1beta1.ConstraintTemplate{}
	err := r.Get(ctx, request.NamespacedName, ct)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	deleted = deleted || !ct.GetDeletionTimestamp().IsZero()

	if deleted {
		ctRef := &templates.ConstraintTemplate{}
		ctRef.SetNamespace(request.Namespace)
		ctRef.SetName(request.Name)
		ctUnversioned, err := r.cfClient.GetTemplate(ctRef)
		result := reconcile.Result{}
		if err != nil {
			logger.Info("missing constraint template in OPA cache, no deletion necessary")
			logAction(ctRef, deletedAction)
			r.metrics.registry.remove(request.NamespacedName)
		} else {
			result, err = r.handleDelete(ctx, ctUnversioned)
			if err != nil {
				logger.Error(err, "deletion error")
				logError(request.NamespacedName.Name)
				r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
				return reconcile.Result{}, err
			}
			if !result.Requeue {
				logAction(ct, deletedAction)
				r.metrics.registry.remove(request.NamespacedName)
			}
		}
		err = r.deleteAllStatus(ctx, request.Name)
		return result, err
	}

	status, err := r.getOrCreatePodStatus(ctx, ct.Name)
	if err != nil {
		logger.Info("could not get/create pod status object", "error", err)
		return reconcile.Result{}, err
	}
	status.Status.TemplateUID = ct.GetUID()
	status.Status.ObservedGeneration = ct.GetGeneration()
	status.Status.Errors = nil

	unversionedCT := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(ct, unversionedCT, nil); err != nil {
		logger.Error(err, "conversion error")
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		logError(request.NamespacedName.Name)
		return reconcile.Result{}, err
	}

	unversionedProposedCRD, err := r.cfClient.CreateCRD(ctx, unversionedCT)
	if err != nil {
		logger.Error(err, "CRD creation error")
		r.tracker.TryCancelTemplate(unversionedCT) // Don't track templates that failed compilation
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)

		createErr := &v1beta1.CreateCRDError{Code: ErrCreateCode, Message: err.Error()}
		status.Status.Errors = append(status.Status.Errors, createErr)

		if updateErr := r.Update(ctx, status); updateErr != nil {
			logger.Error(updateErr, "update status error")
			return reconcile.Result{Requeue: true}, nil
		}
		logError(request.NamespacedName.Name)
		return reconcile.Result{}, nil
	}

	proposedCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.scheme.Convert(unversionedProposedCRD, proposedCRD, nil); err != nil {
		logger.Error(err, "CRD conversion error")
		r.tracker.TryCancelTemplate(unversionedCT) // Don't track templates that failed compilation
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		logError(request.NamespacedName.Name)
		err := r.reportErrorOnCTStatus(ctx, ErrConversionCode, "Could not convert from unversioned resource", status, err)
		return reconcile.Result{}, err
	}

	name := unversionedProposedCRD.GetName()
	namespace := unversionedProposedCRD.GetNamespace()
	// Check if the constraint CRD already exists
	action := updatedAction
	currentCRD := &apiextensionsv1.CustomResourceDefinition{}
	err = r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, currentCRD)
	switch {
	case err == nil:
		break

	case apierrors.IsNotFound(err):
		action = createdAction
		currentCRD = nil

	default:
		logger.Error(err, "client.Get CRD error")
		logError(request.NamespacedName.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		return reconcile.Result{}, err
	}
	err = nil
	generateVap := false
	if constraint.HasVAPCel(ct) {
		generateVap, err = shouldGenerateVAP(unversionedCT, *defaultGenerateVAP)
	}
	if err != nil {
		logger.Info("returning error", "r.generateVap", err)
		return reconcile.Result{}, err
	}
	logger.Info("generateVap", "r.generateVap", generateVap)

	result, err := r.handleUpdate(ctx, ct, unversionedCT, proposedCRD, currentCRD, status, generateVap)
	if err != nil {
		logger.Error(err, "handle update error")
		logError(request.NamespacedName.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		return result, err
	}
	if !result.Requeue {
		logAction(ct, action)
		r.metrics.registry.add(request.NamespacedName, metrics.ActiveStatus)
	}
	return result, err
}

func (r *ReconcileConstraintTemplate) reportErrorOnCTStatus(ctx context.Context, code, message string, status *statusv1beta1.ConstraintTemplatePodStatus, err error) error {
	status.Status.Errors = []*v1beta1.CreateCRDError{}
	createErr := &v1beta1.CreateCRDError{
		Code:    code,
		Message: fmt.Sprintf("%s: %s", message, err),
	}
	status.Status.Errors = append(status.Status.Errors, createErr)
	if err2 := r.Update(ctx, status); err2 != nil {
		return errorpkg.Wrap(err, fmt.Sprintf("Could not update status: %s", err2))
	}
	return err
}

func (r *ReconcileConstraintTemplate) handleUpdate(
	ctx context.Context,
	ct *v1beta1.ConstraintTemplate,
	unversionedCT *templates.ConstraintTemplate,
	proposedCRD, currentCRD *apiextensionsv1.CustomResourceDefinition,
	status *statusv1beta1.ConstraintTemplatePodStatus,
	generateVap bool,
) (reconcile.Result, error) {
	name := proposedCRD.GetName()
	logger := logger.WithValues("name", ct.GetName(), "crdName", name)

	logger.Info("loading code into rule engine")
	beginCompile := time.Now()

	// It's important that cfClient.AddTemplate() is called first. That way we can
	// rely on a template's existence in rule engine to know whether a watch needs
	// to be removed
	if _, err := r.cfClient.AddTemplate(ctx, unversionedCT); err != nil {
		if err := r.metrics.reportIngestDuration(ctx, metrics.ErrorStatus, time.Since(beginCompile)); err != nil {
			logger.Error(err, "failed to report constraint template ingestion duration")
		}
		err := r.reportErrorOnCTStatus(ctx, ErrIngestCode, "Could not ingest Rego", status, err)
		r.tracker.TryCancelTemplate(unversionedCT) // Don't track templates that failed compilation
		return reconcile.Result{}, err
	}

	if err := r.metrics.reportIngestDuration(ctx, metrics.ActiveStatus, time.Since(beginCompile)); err != nil {
		logger.Error(err, "failed to report constraint template ingestion duration")
	}

	// Mark for readiness tracking
	t := r.tracker.For(gvkConstraintTemplate)
	t.Observe(unversionedCT)

	var newCRD *apiextensionsv1.CustomResourceDefinition
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
		logger.Info("creating crd")
		if err := r.Create(ctx, newCRD); err != nil {
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not create CRD", status, err)
			return reconcile.Result{}, err
		}
	} else if !reflect.DeepEqual(newCRD, currentCRD) {
		logger.Info("updating crd")
		if err := r.Update(ctx, newCRD); err != nil {
			err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not update CRD", status, err)
			return reconcile.Result{}, err
		}
	}
	// This must go after CRD creation/update as otherwise AddWatch will always fail
	logger.Info("making sure constraint is in watcher registry")
	if err := r.addWatch(ctx, makeGvk(ct.Spec.CRD.Spec.Names.Kind)); err != nil {
		logger.Error(err, "error adding template to watch registry")
		return reconcile.Result{}, err
	}
	// generating vap resources
	if generateVap && constraint.IsVapAPIEnabled() {
		currentVap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
		vapName := fmt.Sprintf("gatekeeper-%s", unversionedCT.GetName())
		logger.Info("check if vap exists", "vapName", vapName)
		if err := r.Get(ctx, types.NamespacedName{Name: vapName}, currentVap); err != nil {
			if !apierrors.IsNotFound(err) && !errors.As(err, &discoveryErr) && !meta.IsNoMatchError(err) {
				return reconcile.Result{}, err
			}
			currentVap = nil
		}
		logger.Info("get vap", "vapName", vapName, "currentVap", currentVap)
		newVap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
		transformedVap, err := transform.TemplateToPolicyDefinition(unversionedCT)
		if err != nil {
			logger.Info("transform to vap error", "vapName", vapName, "error", err)
			createErr := &v1beta1.CreateCRDError{Code: ErrCreateCode, Message: err.Error()}
			status.Status.Errors = append(status.Status.Errors, createErr)
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not transform to vap object", status, err)
			return reconcile.Result{}, err
		}
		if currentVap == nil {
			newVap = transformedVap.DeepCopy()
		} else {
			newVap = currentVap.DeepCopy()
			newVap.Spec = transformedVap.Spec
		}

		if err := controllerutil.SetControllerReference(ct, newVap, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		if currentVap == nil {
			logger.Info("creating vap", "vapName", vapName)
			if err := r.Create(ctx, newVap); err != nil {
				logger.Info("creating vap error", "vapName", vapName, "error", err)
				createErr := &v1beta1.CreateCRDError{Code: ErrCreateCode, Message: err.Error()}
				status.Status.Errors = append(status.Status.Errors, createErr)
				err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not create vap object", status, err)
				return reconcile.Result{}, err
			}
			// after vap is created, trigger update event for all constraints
			if err := r.triggerConstraintEvents(ctx, ct, status); err != nil {
				return reconcile.Result{}, err
			}
		} else if !reflect.DeepEqual(currentVap, newVap) {
			logger.Info("updating vap")
			if err := r.Update(ctx, newVap); err != nil {
				updateErr := &v1beta1.CreateCRDError{Code: ErrUpdateCode, Message: err.Error()}
				status.Status.Errors = append(status.Status.Errors, updateErr)
				err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not update vap object", status, err)
				return reconcile.Result{}, err
			}
		}
	}
	// do not generate vap resources
	// remove if exists
	if !generateVap && constraint.IsVapAPIEnabled() {
		currentVap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
		vapName := fmt.Sprintf("gatekeeper-%s", unversionedCT.GetName())
		logger.Info("check if vap exists", "vapName", vapName)
		if err := r.Get(ctx, types.NamespacedName{Name: vapName}, currentVap); err != nil {
			if !apierrors.IsNotFound(err) && !errors.As(err, &discoveryErr) && !meta.IsNoMatchError(err) {
				return reconcile.Result{}, err
			}
			currentVap = nil
		}
		if currentVap != nil {
			logger.Info("deleting vap")
			if err := r.Delete(ctx, currentVap); err != nil {
				updateErr := &v1beta1.CreateCRDError{Code: ErrUpdateCode, Message: err.Error()}
				status.Status.Errors = append(status.Status.Errors, updateErr)
				err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not delete vap object", status, err)
				return reconcile.Result{}, err
			}
			// after vap is deleted, trigger update event for all constraints
			if err := r.triggerConstraintEvents(ctx, ct, status); err != nil {
				return reconcile.Result{}, err
			}
		}
	}
	if err := r.Update(ctx, status); err != nil {
		logger.Error(err, "update ct pod status error")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraintTemplate) handleDelete(
	ctx context.Context,
	ct *templates.ConstraintTemplate,
) (reconcile.Result, error) {
	logger := logger.WithValues("name", ct.GetName())
	logger.Info("removing from watcher registry")
	gvk := makeGvk(ct.Spec.CRD.Spec.Names.Kind)
	if err := r.removeWatch(ctx, gvk); err != nil {
		return reconcile.Result{}, err
	}
	r.tracker.CancelTemplate(ct)

	// removing the template from the OPA cache must go last as we are relying
	// on that cache to derive the Kind to remove from the watch
	if _, err := r.cfClient.RemoveTemplate(ctx, ct); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileConstraintTemplate) defaultGetPod(_ context.Context) (*corev1.Pod, error) {
	// require injection of GetPod in order to control what client we use to
	// guarantee we don't inadvertently create a watch
	panic("GetPod must be injected to ReconcileConstraintTemplate")
}

func (r *ReconcileConstraintTemplate) deleteAllStatus(ctx context.Context, ctName string) error {
	statusObj := &statusv1beta1.ConstraintTemplatePodStatus{}
	sName, err := statusv1beta1.KeyForConstraintTemplate(util.GetPodName(), ctName)
	if err != nil {
		return err
	}
	statusObj.SetName(sName)
	statusObj.SetNamespace(util.GetNamespace())
	if err := r.Delete(ctx, statusObj); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	cstrStatusObjs := &statusv1beta1.ConstraintPodStatusList{}
	if err := r.List(ctx, cstrStatusObjs, client.MatchingLabels(map[string]string{
		statusv1beta1.PodLabel:                    util.GetPodName(),
		statusv1beta1.ConstraintTemplateNameLabel: ctName,
	})); err != nil {
		return err
	}
	for index := range cstrStatusObjs.Items {
		if err := r.Delete(ctx, &cstrStatusObjs.Items[index]); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

func (r *ReconcileConstraintTemplate) getOrCreatePodStatus(ctx context.Context, ctName string) (*statusv1beta1.ConstraintTemplatePodStatus, error) {
	statusObj := &statusv1beta1.ConstraintTemplatePodStatus{}
	sName, err := statusv1beta1.KeyForConstraintTemplate(util.GetPodName(), ctName)
	if err != nil {
		return nil, err
	}
	key := types.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
	if err := r.Get(ctx, key, statusObj); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
	} else {
		return statusObj, nil
	}
	pod, err := r.getPod(ctx)
	if err != nil {
		return nil, err
	}
	statusObj, err = statusv1beta1.NewConstraintTemplateStatusForPod(pod, ctName, r.scheme)
	if err != nil {
		return nil, err
	}
	if err := r.Create(ctx, statusObj); err != nil {
		return nil, err
	}
	return statusObj, nil
}

func (r *ReconcileConstraintTemplate) addWatch(ctx context.Context, kind schema.GroupVersionKind) error {
	if err := r.watcher.AddWatch(ctx, kind); err != nil {
		return err
	}
	return r.statusWatcher.AddWatch(ctx, kind)
}

func (r *ReconcileConstraintTemplate) removeWatch(ctx context.Context, kind schema.GroupVersionKind) error {
	if err := r.watcher.RemoveWatch(ctx, kind); err != nil {
		return err
	}
	return r.statusWatcher.RemoveWatch(ctx, kind)
}

func (r *ReconcileConstraintTemplate) listObjects(ctx context.Context, gvk schema.GroupVersionKind) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{
		Object: map[string]interface{}{},
		Items:  []unstructured.Unstructured{},
	}
	gvk.Kind += "List"
	list.SetGroupVersionKind(gvk)
	err := r.List(ctx, list)
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *ReconcileConstraintTemplate) triggerConstraintEvents(ctx context.Context, ct *v1beta1.ConstraintTemplate, status *statusv1beta1.ConstraintTemplatePodStatus) error {
	gvk := makeGvk(ct.Spec.CRD.Spec.Names.Kind)
	logger.Info("list gvk objects", "gvk", gvk)
	cstrObjs, err := r.listObjects(ctx, gvk)
	if err != nil {
		logger.Error(err, "get all constraints listObjects")
		updateErr := &v1beta1.CreateCRDError{Code: ErrUpdateCode, Message: err.Error()}
		status.Status.Errors = append(status.Status.Errors, updateErr)
		err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not list all constraint objects", status, err)
		return err
	}
	logger.Info("list gvk objects", "cstrObjs", cstrObjs)
	for _, cstr := range cstrObjs {
		c := cstr
		logger.Info("triggering cstrEvent")
		r.cstrEvents <- event.GenericEvent{Object: &c}
	}

	return nil
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
	logger.Info(
		fmt.Sprintf("template was %s", string(a)),
		logging.EventType, fmt.Sprintf("template_%s", string(a)),
		logging.TemplateName, template.GetName(),
	)
}

func logError(name string) {
	logger.Info(
		"unable to ingest template",
		logging.EventType, "template_ingest_error",
		logging.TemplateName, name,
	)
}

func makeGvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    kind,
	}
}

func shouldGenerateVAP(ct *templates.ConstraintTemplate, generateVAPDefault bool) (bool, error) {
	source, err := pSchema.GetSourceFromTemplate(ct)
	if err != nil {
		return false, err
	}
	if source.GenerateVAP == nil {
		return generateVAPDefault, nil
	}
	return *source.GenerateVAP, nil
}
