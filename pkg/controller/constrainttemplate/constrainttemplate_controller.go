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
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraintstatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constrainttemplatestatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	errorpkg "github.com/pkg/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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
	logger       = log.Log.V(logging.DebugLevel).WithName("controller").WithValues("kind", "ConstraintTemplate", logging.Process, "constraint_template_controller")
	discoveryErr *apiutil.ErrResourceDiscoveryFailed
)

var gvkConstraintTemplate = schema.GroupVersionKind{
	Group:   v1beta1.SchemeGroupVersion.Group,
	Version: v1beta1.SchemeGroupVersion.Version,
	Kind:    "ConstraintTemplate",
}

type Adder struct {
	CFClient           *constraintclient.Client
	WatchManager       *watch.Manager
	Tracker            *readiness.Tracker
	ProcessExcluder    *process.Excluder
	GetPod             func(context.Context) (*corev1.Pod, error)
	WebhookConfigCache *webhookconfigcache.WebhookConfigCache
	CtEvents           <-chan event.GenericEvent
}

// Add creates a new ConstraintTemplate Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.HasValidationOperations() {
		return nil
	}
	// constraintEvents will be used to receive events from dynamic watches registered for constraint controller
	constraintEvents := make(chan event.GenericEvent, 1024)
	r, err := newReconciler(mgr, a.CFClient, a.WatchManager, a.Tracker, constraintEvents, constraintEvents, a.GetPod, a.WebhookConfigCache, a.ProcessExcluder)
	if err != nil {
		return err
	}
	return add(mgr, r, a.CtEvents)
}

func (a *Adder) InjectCFClient(c *constraintclient.Client) {
	a.CFClient = c
}

func (a *Adder) InjectWatchManager(wm *watch.Manager) {
	a.WatchManager = wm
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectGetPod(getPod func(context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

func (a *Adder) InjectProcessExcluder(m *process.Excluder) {
	a.ProcessExcluder = m
}

func (a *Adder) InjectWebhookConfigCache(wcc *webhookconfigcache.WebhookConfigCache) {
	a.WebhookConfigCache = wcc
}

func (a *Adder) InjectConstraintTemplateEvent(ctEvents chan event.GenericEvent) {
	a.CtEvents = ctEvents
}

// newReconciler returns a new reconcile.Reconciler
// cstrEvents is the channel from which constraint controller will receive the events
// regEvents is the channel registered by Registrar to put the events in
// cstrEvents and regEvents point to same event channel except for testing.
func newReconciler(mgr manager.Manager, cfClient *constraintclient.Client, wm *watch.Manager, tracker *readiness.Tracker, cstrEvents chan event.GenericEvent, regEvents chan<- event.GenericEvent, getPod func(context.Context) (*corev1.Pod, error), webhookCache *webhookconfigcache.WebhookConfigCache, processExcluder *process.Excluder) (*ReconcileConstraintTemplate, error) {
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
			CFClient:     cfClient,
			WatchManager: wm,
			Events:       statusEvents,
			IfWatching:   statusW.IfWatching,
		}
		if err := csAdder.Add(mgr); err != nil {
			return nil, err
		}

		ctsAdder := constrainttemplatestatus.Adder{
			CfClient:     cfClient,
			WatchManager: wm,
		}
		if err := ctsAdder.Add(mgr); err != nil {
			return nil, err
		}
	}

	r := newStatsReporter()
	reconciler := &ReconcileConstraintTemplate{
		Client:          mgr.GetClient(),
		scheme:          mgr.GetScheme(),
		cfClient:        cfClient,
		watcher:         w,
		statusWatcher:   statusW,
		metrics:         r,
		tracker:         tracker,
		getPod:          getPod,
		cstrEvents:      regEvents,
		webhookCache:    webhookCache,
		processExcluder: processExcluder,
	}

	if getPod == nil {
		reconciler.getPod = reconciler.defaultGetPod
	}
	return reconciler, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler, events <-chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New(ctrlName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to ConstraintTemplate
	err = c.Watch(source.Kind(mgr.GetCache(), &v1beta1.ConstraintTemplate{}, &handler.TypedEnqueueRequestForObject[*v1beta1.ConstraintTemplate]{}))
	if err != nil {
		return err
	}

	// Watch for webhook configuration change events (only if Generate operation is enabled)
	if operations.IsAssigned(operations.Generate) && *transform.SyncVAPScope && events != nil {
		err = c.Watch(source.Channel(events, &handler.EnqueueRequestForObject{}))
		if err != nil {
			return err
		}
	}

	// Watch for changes to ConstraintTemplateStatus
	err = c.Watch(
		source.Kind(mgr.GetCache(), &statusv1beta1.ConstraintTemplatePodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(constrainttemplatestatus.PodStatusToConstraintTemplateMapper(true))))
	if err != nil {
		return err
	}

	// Watch for changes to Constraint CRDs
	err = c.Watch(
		source.Kind(mgr.GetCache(), &apiextensionsv1.CustomResourceDefinition{},
			handler.TypedEnqueueRequestForOwner[*apiextensionsv1.CustomResourceDefinition](
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&v1beta1.ConstraintTemplate{},
				handler.OnlyControllerOwner(),
			)))
	if err != nil {
		return err
	}

	isVapAPIEnabled, groupVersion := transform.IsVapAPIEnabled(&logger)
	if isVapAPIEnabled && operations.IsAssigned(operations.Generate) {
		obj, err := vapForVersion(groupVersion)
		if err != nil {
			return err
		}
		err = c.Watch(
			source.Kind(mgr.GetCache(), obj,
				handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
					var out []reconcile.Request
					for _, owner := range obj.GetOwnerReferences() {
						if owner.Controller != nil && *owner.Controller && strings.HasPrefix(owner.APIVersion, v1beta1.SchemeGroupVersion.Group+"/") {
							out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Name: owner.Name}})
						}
					}
					return out
				})))
		if err != nil {
			return err
		}
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileConstraintTemplate{}

// ReconcileConstraintTemplate reconciles a ConstraintTemplate object.
type ReconcileConstraintTemplate struct {
	client.Client
	scheme          *runtime.Scheme
	watcher         *watch.Registrar
	statusWatcher   *watch.Registrar
	cfClient        *constraintclient.Client
	metrics         *reporter
	tracker         *readiness.Tracker
	getPod          func(context.Context) (*corev1.Pod, error)
	cstrEvents      chan<- event.GenericEvent
	webhookCache    *webhookconfigcache.WebhookConfigCache
	processExcluder *process.Excluder
}

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingadmissionpolicies;validatingadmissionpolicybindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=externaldata.gatekeeper.sh,resources=providers,verbs=get;list;watch;create;update;patch;delete
// update permission on finalizers is needed to access metadata.ownerReferences[x].blockOwnerDeletion for OwnerReferencesPermissionEnforcement admission plugin - https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#ownerreferencespermissionenforcement.
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/finalizers,verbs=update

// Reconcile reads that state of the cluster for a ConstraintTemplate object and makes changes based on the state read
// and what is in the ConstraintTemplate.Spec.
func (r *ReconcileConstraintTemplate) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := logger.WithValues("template_name", request.Name)

	defer r.metrics.registry.report(ctx, r.metrics)

	// Fetch the ConstraintTemplate instance
	deleted := false
	ct := &v1beta1.ConstraintTemplate{}
	// TODO - validate that false reconcile requests are not happening, check that this reconciler is not getting triggered for events meant to be for constraint controller
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
				logError(request.Name)
				r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
				return reconcile.Result{}, err
			}
			if result.RequeueAfter == 0 {
				logAction(ct, deletedAction)
				r.metrics.registry.remove(request.NamespacedName)
			}
			isAPIEnabled, groupVersion := transform.IsVapAPIEnabled(&logger)
			if isAPIEnabled {
				currentVap, err := vapForVersion(groupVersion)
				if err != nil {
					return reconcile.Result{}, err
				}
				vapName := getVAPName(ctUnversioned.GetName())
				currentVap.SetName(vapName)
				if err := r.Delete(ctx, currentVap); err != nil {
					if !apierrors.IsNotFound(err) {
						return reconcile.Result{}, err
					}
				}
			}
		}
		err = r.deleteAllStatus(ctx, request.Name)
		return result, err
	}

	unversionedCT := &templates.ConstraintTemplate{}
	if err := r.scheme.Convert(ct, unversionedCT, nil); err != nil {
		logger.Error(err, "conversion error")
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		logError(request.Name)
		return reconcile.Result{}, err
	}

	status, err := r.getOrCreatePodStatus(ctx, ct.Name)
	if err != nil {
		logger.Info("could not get/create pod status object", "error", err)
		return reconcile.Result{}, err
	}
	status.Status.TemplateUID = ct.GetUID()
	status.Status.ObservedGeneration = ct.GetGeneration()
	status.Status.Errors = nil

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
		logError(request.Name)
		return reconcile.Result{}, nil
	}

	proposedCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.scheme.Convert(unversionedProposedCRD, proposedCRD, nil); err != nil {
		logger.Error(err, "CRD conversion error")
		r.tracker.TryCancelTemplate(unversionedCT) // Don't track templates that failed compilation
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		logError(request.Name)
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
		logError(request.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		return reconcile.Result{}, err
	}

	result, err := r.handleUpdate(ctx, ct, unversionedCT, proposedCRD, currentCRD, status)
	if err != nil {
		logger.Error(err, "handle update error")
		logError(request.Name)
		r.metrics.registry.add(request.NamespacedName, metrics.ErrorStatus)
		return result, err
	}
	if result.RequeueAfter == 0 {
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

	generateVap, err := constraint.ShouldGenerateVAP(unversionedCT)
	if err != nil && !errors.Is(err, celSchema.ErrCELEngineMissing) {
		logger.Error(err, "generateVap error")
		status.Status.VAPGenerationStatus = &statusv1beta1.VAPGenerationStatus{State: ErrGenerateVAPState, ObservedGeneration: ct.GetGeneration(), Warning: fmt.Sprintf("ValidatingAdmissionPolicy is not generated: %s", err.Error())}
	}

	var requeueAfter time.Duration
	if err := r.generateCRD(ctx, ct, proposedCRD, currentCRD, status, logger, generateVap, &requeueAfter); err != nil {
		return reconcile.Result{}, err
	}

	// This must go after CRD creation/update as otherwise AddWatch will always fail
	logger.Info("making sure constraint is in watcher registry")
	if err := r.addWatch(ctx, makeGvk(ct.Spec.CRD.Spec.Names.Kind)); err != nil {
		logger.Error(err, "error adding template to watch registry")
		return reconcile.Result{}, err
	}

	err = r.manageVAP(ctx, ct, unversionedCT, status, logger, generateVap)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := r.Update(ctx, status); err != nil {
		logger.Error(err, "update ct pod status error")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{RequeueAfter: requeueAfter}, nil
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

func vapForVersion(gvk *schema.GroupVersion) (client.Object, error) {
	switch gvk.Version {
	case "v1":
		return &admissionregistrationv1.ValidatingAdmissionPolicy{}, nil
	case "v1beta1":
		return &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}, nil
	default:
		return nil, errors.New("unrecognized version")
	}
}

func getVAPName(constraintName string) string {
	return fmt.Sprintf("gatekeeper-%s", constraintName)
}

func getRunTimeVAP(gvk *schema.GroupVersion, transformedVap *admissionregistrationv1beta1.ValidatingAdmissionPolicy, currentVap client.Object) (client.Object, error) {
	if currentVap == nil {
		if gvk.Version == "v1" {
			return v1beta1ToV1(transformedVap)
		}
		return transformedVap.DeepCopy(), nil
	}

	if gvk.Version == "v1" {
		v1CurrentVAP, ok := currentVap.(*admissionregistrationv1.ValidatingAdmissionPolicy)
		if !ok {
			return nil, errors.New("unable to convert to v1 VAP")
		}
		v1CurrentVAP = v1CurrentVAP.DeepCopy()
		tempVAP, err := v1beta1ToV1(transformedVap)
		if err != nil {
			return nil, err
		}
		v1CurrentVAP.Spec = tempVAP.Spec
		return v1CurrentVAP, nil
	}

	v1beta1VAP, ok := currentVap.(*admissionregistrationv1beta1.ValidatingAdmissionPolicy)
	if !ok {
		return nil, errors.New("unable to convert to v1 VAP")
	}
	v1beta1VAP.Spec = transformedVap.Spec
	return v1beta1VAP.DeepCopy(), nil
}

func v1beta1ToV1(v1beta1Obj *admissionregistrationv1beta1.ValidatingAdmissionPolicy) (*admissionregistrationv1.ValidatingAdmissionPolicy, error) {
	// TODO(jgabani): Use r.scheme.Convert to convert from v1beta1 to v1 once the conversion bug is fixed - https://github.com/kubernetes/kubernetes/issues/126582
	obj := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	obj.SetName(v1beta1Obj.GetName())
	obj.Spec.ParamKind = &admissionregistrationv1.ParamKind{
		APIVersion: v1beta1Obj.Spec.ParamKind.APIVersion,
		Kind:       v1beta1Obj.Spec.ParamKind.Kind,
	}

	// Convert MatchConstraints from v1beta1 to v1
	if v1beta1Obj.Spec.MatchConstraints != nil {
		matchConstraints := &admissionregistrationv1.MatchResources{}

		if v1beta1Obj.Spec.MatchConstraints.NamespaceSelector != nil {
			matchConstraints.NamespaceSelector = v1beta1Obj.Spec.MatchConstraints.NamespaceSelector
		}

		if v1beta1Obj.Spec.MatchConstraints.ObjectSelector != nil {
			matchConstraints.ObjectSelector = v1beta1Obj.Spec.MatchConstraints.ObjectSelector
		}

		if v1beta1Obj.Spec.MatchConstraints.MatchPolicy != nil {
			matchPolicy := admissionregistrationv1.MatchPolicyType(*v1beta1Obj.Spec.MatchConstraints.MatchPolicy)
			matchConstraints.MatchPolicy = &matchPolicy
		}

		// Convert ResourceRules
		for i := range v1beta1Obj.Spec.MatchConstraints.ResourceRules {
			rule := &v1beta1Obj.Spec.MatchConstraints.ResourceRules[i]
			// Convert operations
			operations := make([]admissionregistrationv1.OperationType, 0, len(rule.Operations))
			operations = append(operations, rule.Operations...)

			v1Rule := admissionregistrationv1.NamedRuleWithOperations{
				RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
					Operations: operations,
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   rule.APIGroups,
						APIVersions: rule.APIVersions,
						Resources:   rule.Resources,
					},
				},
			}
			matchConstraints.ResourceRules = append(matchConstraints.ResourceRules, v1Rule)
		}

		obj.Spec.MatchConstraints = matchConstraints
	}

	obj.Spec.MatchConditions = []admissionregistrationv1.MatchCondition{}

	for _, matchCondition := range v1beta1Obj.Spec.MatchConditions {
		obj.Spec.MatchConditions = append(obj.Spec.MatchConditions, admissionregistrationv1.MatchCondition{
			Name:       matchCondition.Name,
			Expression: matchCondition.Expression,
		})
	}

	obj.Spec.Validations = []admissionregistrationv1.Validation{}

	for _, v := range v1beta1Obj.Spec.Validations {
		obj.Spec.Validations = append(obj.Spec.Validations, admissionregistrationv1.Validation{
			Expression:        v.Expression,
			Message:           v.Message,
			MessageExpression: v.MessageExpression,
		})
	}

	var failurePolicy admissionregistrationv1.FailurePolicyType
	switch *v1beta1Obj.Spec.FailurePolicy {
	case admissionregistrationv1beta1.Ignore:
		failurePolicy = admissionregistrationv1.Ignore
	case admissionregistrationv1beta1.Fail:
		failurePolicy = admissionregistrationv1.Fail
	default:
		return nil, fmt.Errorf("%w: unrecognized failure policy: %s", celSchema.ErrBadFailurePolicy, *v1beta1Obj.Spec.FailurePolicy)
	}
	obj.Spec.FailurePolicy = &failurePolicy
	obj.Spec.AuditAnnotations = nil

	for _, v := range v1beta1Obj.Spec.Variables {
		obj.Spec.Variables = append(obj.Spec.Variables, admissionregistrationv1.Variable{
			Name:       v.Name,
			Expression: v.Expression,
		})
	}

	return obj, nil
}

func (r *ReconcileConstraintTemplate) generateCRD(ctx context.Context, ct *v1beta1.ConstraintTemplate, proposedCRD, currentCRD *apiextensionsv1.CustomResourceDefinition, status *statusv1beta1.ConstraintTemplatePodStatus, logger logr.Logger, generateVAP bool, requeueAfter *time.Duration) error {
	if !operations.IsAssigned(operations.Generate) {
		return nil
	}
	var newCRD *apiextensionsv1.CustomResourceDefinition
	if currentCRD == nil {
		newCRD = proposedCRD.DeepCopy()
	} else {
		newCRD = currentCRD.DeepCopy()
		newCRD.Spec = proposedCRD.Spec
	}

	if err := controllerutil.SetControllerReference(ct, newCRD, r.scheme); err != nil {
		return err
	}

	if currentCRD == nil {
		logger.Info("creating crd")
		if err := r.Create(ctx, newCRD); err != nil {
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not create CRD", status, err)
			return err
		}
	} else if !reflect.DeepEqual(newCRD, currentCRD) {
		logger.Info("updating crd")
		if err := r.Update(ctx, newCRD); err != nil {
			err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not update CRD", status, err)
			return err
		}
	}
	if !generateVAP {
		return nil
	}
	var err error
	// We add the annotation as a follow-on update to be sure the timestamp is set relative to a time after the CRD is successfully created. Creating the CRD with a delay timestamp already set would not account for request latency.
	var duration time.Duration
	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Fetch the latest version of the ConstraintTemplate before updating
		latestCT := &v1beta1.ConstraintTemplate{}
		if getErr := r.Get(ctx, types.NamespacedName{Name: ct.GetName()}, latestCT); getErr != nil {
			return getErr
		}
		duration, err = r.updateTemplateWithBlockVAPBGenerationAnnotations(ctx, latestCT)
		return err
	})
	*requeueAfter = duration
	if retryErr != nil {
		err = r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not annotate with timestamp to block VAPB generation", status, retryErr)
	}
	return err
}

func (r *ReconcileConstraintTemplate) manageVAP(ctx context.Context, ct *v1beta1.ConstraintTemplate, unversionedCT *templates.ConstraintTemplate, status *statusv1beta1.ConstraintTemplatePodStatus, logger logr.Logger, generateVap bool) error {
	if !operations.IsAssigned(operations.Generate) {
		logger.Info("generate operation is not assigned, ValidatingAdmissionPolicy resource will not be generated")
		return nil
	}
	isVapAPIEnabled := false
	var groupVersion *schema.GroupVersion
	logger.Info("generateVap", "r.generateVap", generateVap)

	isVapAPIEnabled, groupVersion = transform.IsVapAPIEnabled(&logger)
	logger.Info("isVapAPIEnabled", "isVapAPIEnabled", isVapAPIEnabled)
	logger.Info("groupVersion", "groupVersion", groupVersion)

	if generateVap && (!isVapAPIEnabled || groupVersion == nil) {
		logger.Error(constraint.ErrValidatingAdmissionPolicyAPIDisabled, "ValidatingAdmissionPolicy resource cannot be generated for ConstraintTemplate", "name", ct.GetName())
		err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "ValidatingAdmissionPolicy resource cannot be generated for ConstraintTemplate", status, constraint.ErrValidatingAdmissionPolicyAPIDisabled)
		return err
	}
	// generating VAP resources
	if generateVap && isVapAPIEnabled && groupVersion != nil {
		currentVap, err := vapForVersion(groupVersion)
		if err != nil {
			logger.Error(err, "error getting VAP object with respective groupVersion")
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not get VAP with runtime group version", status, err)
			return err
		}
		vapName := getVAPName(unversionedCT.GetName())
		logger.Info("check if VAP exists", "vapName", vapName)
		if err := r.Get(ctx, types.NamespacedName{Name: vapName}, currentVap); err != nil {
			if !apierrors.IsNotFound(err) && !errors.As(err, &discoveryErr) && !meta.IsNoMatchError(err) {
				err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not get VAP object", status, err)
				return err
			}
			currentVap = nil
		}
		logger.Info("get VAP", "vapName", vapName, "currentVap", currentVap)

		transformedVap, err := r.transformTemplateToVAP(unversionedCT, vapName, logger)
		if err != nil {
			logger.Error(err, "transform to VAP error", "vapName", vapName)
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not transform to VAP object", status, err)
			return err
		}

		newVap, err := getRunTimeVAP(groupVersion, transformedVap, currentVap)
		if err != nil {
			logger.Error(err, "getRunTimeVAP error", "vapName", vapName)
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not get runtime VAP object", status, err)
			return err
		}

		if err := controllerutil.SetControllerReference(ct, newVap, r.scheme); err != nil {
			return err
		}

		if currentVap == nil {
			logger.Info("creating VAP", "vapName", vapName)
			if err := r.Create(ctx, newVap); err != nil {
				logger.Info("creating VAP error", "vapName", vapName, "error", err)
				err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not create VAP object", status, err)
				return err
			}
			// after VAP is created, trigger update event for all constraints
			if err := r.triggerConstraintEvents(ctx, ct, status); err != nil {
				return err
			}
		} else if !reflect.DeepEqual(currentVap, newVap) {
			logger.Info("updating VAP")
			if err := r.Update(ctx, newVap); err != nil {
				err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not update VAP object", status, err)
				return err
			}
		}
		status.Status.VAPGenerationStatus = &statusv1beta1.VAPGenerationStatus{State: GeneratedVAPState, ObservedGeneration: ct.GetGeneration(), Warning: ""}
	}
	// do not generate VAP resources
	// remove if exists
	if !generateVap && isVapAPIEnabled && groupVersion != nil {
		currentVap, err := vapForVersion(groupVersion)
		if err != nil {
			logger.Error(err, "error getting VAP object with respective groupVersion")
			err := r.reportErrorOnCTStatus(ctx, ErrCreateCode, "Could not get VAP with correct group version", status, err)
			return err
		}
		vapName := getVAPName(unversionedCT.GetName())
		logger.Info("check if VAP exists", "vapName", vapName)
		if err := r.Get(ctx, types.NamespacedName{Name: vapName}, currentVap); err != nil {
			if !apierrors.IsNotFound(err) && !errors.As(err, &discoveryErr) && !meta.IsNoMatchError(err) {
				return err
			}
			currentVap = nil
		}
		if currentVap != nil {
			logger.Info("deleting VAP")
			if err := r.Delete(ctx, currentVap); err != nil {
				err := r.reportErrorOnCTStatus(ctx, ErrUpdateCode, "Could not delete VAP object", status, err)
				return err
			}
			status.Status.VAPGenerationStatus = nil
			// after VAP is deleted, trigger update event for all constraints
			if err := r.triggerConstraintEvents(ctx, ct, status); err != nil {
				return err
			}
		}
	}
	return nil
}

// updateTemplateWithBlockVAPBGenerationAnnotations updates the ConstraintTemplate with an annotation to block VAPB generation until specific time
// This is to avoid the issue where the VAPB is generated before the CRD is cached in the API server.
func (r *ReconcileConstraintTemplate) updateTemplateWithBlockVAPBGenerationAnnotations(ctx context.Context, ct *v1beta1.ConstraintTemplate) (time.Duration, error) {
	noRequeue := time.Duration(0)
	if ct.Annotations != nil && ct.Annotations[constraint.VAPBGenerationAnnotation] == constraint.VAPBGenerationUnblocked {
		return noRequeue, nil
	}
	currentTime := time.Now()
	if ct.Annotations != nil && ct.Annotations[constraint.BlockVAPBGenerationUntilAnnotation] != "" {
		until := ct.Annotations[constraint.BlockVAPBGenerationUntilAnnotation]
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return noRequeue, err
		}
		// if wait time is within the time window to generate vap binding, do not update the annotation
		// otherwise update the annotation with the current time + wait time. This prevents clock skew from preventing generation on task reschedule.
		if t.Before(currentTime.Add(time.Duration(*constraint.DefaultWaitForVAPBGeneration) * time.Second)) {
			if t.Before(currentTime) {
				ct.Annotations[constraint.VAPBGenerationAnnotation] = constraint.VAPBGenerationUnblocked
				return noRequeue, r.Update(ctx, ct)
			}
			return t.Sub(currentTime), nil
		}
	}
	if ct.Annotations == nil {
		ct.Annotations = make(map[string]string)
	}
	ct.Annotations[constraint.BlockVAPBGenerationUntilAnnotation] = currentTime.Add(time.Duration(*constraint.DefaultWaitForVAPBGeneration) * time.Second).Format(time.RFC3339)
	ct.Annotations[constraint.VAPBGenerationAnnotation] = constraint.VAPBGenerationBlocked
	return time.Duration(*constraint.DefaultWaitForVAPBGeneration) * time.Second, r.Update(ctx, ct)
}

func ShouldGenerateVAPForVersionedCT(ct *v1beta1.ConstraintTemplate, scheme *runtime.Scheme) (bool, error) {
	unversionedCT := &templates.ConstraintTemplate{}
	if err := scheme.Convert(ct, unversionedCT, nil); err != nil {
		logger.Error(err, "conversion error")
		return false, err
	}
	return constraint.ShouldGenerateVAP(unversionedCT)
}

// transformTemplateToVAP transforms a ConstraintTemplate to a ValidatingAdmissionPolicy
// It handles both synced webhook scope and default configurations.
func (r *ReconcileConstraintTemplate) transformTemplateToVAP(
	unversionedCT *templates.ConstraintTemplate,
	vapName string,
	logger logr.Logger,
) (*admissionregistrationv1beta1.ValidatingAdmissionPolicy, error) {
	// Use default configuration if SyncVAPScope is not enabled
	if !*transform.SyncVAPScope {
		logger.Info("using default configuration for VAP matching", "vapName", vapName)
		return transform.TemplateToPolicyDefinition(unversionedCT)
	}

	var excludedNamespaces []string
	if r.processExcluder != nil {
		excludedNamespaces = r.processExcluder.GetExcludedNamespaces(process.Webhook)
	}

	exemptedNamespaces := webhook.GetAllExemptedNamespacesWithWildcard()

	webhookConfig := r.getWebhookConfigFromCache(logger)

	return transform.TemplateToPolicyDefinitionWithWebhookConfig(
		unversionedCT,
		webhookConfig,
		excludedNamespaces,
		exemptedNamespaces,
	)
}

// getWebhookConfigFromCache retrieves webhook configuration from cache
// Returns nil if cache is unavailable or config not found.
func (r *ReconcileConstraintTemplate) getWebhookConfigFromCache(logger logr.Logger) *webhookconfigcache.WebhookMatchingConfig {
	if r.webhookCache == nil {
		logger.Info("webhook cache is nil, VAP will be created with default match constraints")
		return nil
	}

	config, exists := r.webhookCache.GetConfig(*webhook.VwhName)
	if !exists {
		logger.Info("webhook config not found in cache, VAP will be created with default match constraints", "lookupKey", *webhook.VwhName)
		return nil
	}

	return &config
}
