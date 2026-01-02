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
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	constraintstatusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraintstatus"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	errorpkg "github.com/pkg/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
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
	BlockVAPBGenerationUntilAnnotation = "gatekeeper.sh/block-vapb-generation-until"
	VAPBGenerationAnnotation           = "gatekeeper.sh/vapb-generation-state"
	ErrGenerateVAPBState               = "error"
	GeneratedVAPBState                 = "generated"
	WaitVAPBState                      = "waiting"
	VAPBGenerationBlocked              = "blocked"
	VAPBGenerationUnblocked            = "unblocked"
)

var (
	log                          = logf.Log.V(logging.DebugLevel).WithName("controller").WithValues(logging.Process, "constraint_controller")
	discoveryErr                 *apiutil.ErrResourceDiscoveryFailed
	DefaultGenerateVAPB          = flag.Bool("default-create-vap-binding-for-constraints", true, "(beta) Create VAPBinding resource for constraint of the template containing VAP-style CEL source. Allowed values are false: do not create Validating Admission Policy Binding, true: create Validating Admission Policy Binding.")
	DefaultGenerateVAP           = flag.Bool("default-create-vap-for-templates", true, "(beta) Create VAP resource for template containing VAP-style CEL source. Allowed values are false: do not create Validating Admission Policy unless generateVAP: true is set on constraint template explicitly, true: create Validating Admission Policy unless generateVAP: false is set on constraint template explicitly.")
	DefaultWaitForVAPBGeneration = flag.Int("default-wait-for-vapb-generation", 30, "(beta) Wait time in seconds before generating a ValidatingAdmissionPolicyBinding after a constraint CRD is created.")
)

var (
	ErrValidatingAdmissionPolicyAPIDisabled = errors.New("validatingAdmissionPolicy API is not enabled")
	ErrVAPConditionsNotSatisfied            = errors.New("conditions are not satisfied to generate ValidatingAdmissionPolicy and ValidatingAdmissionPolicyBinding")
)

type Adder struct {
	CFClient         *constraintclient.Client
	ConstraintsCache *ConstraintsCache
	WatchManager     *watch.Manager
	Events           <-chan event.GenericEvent
	Tracker          *readiness.Tracker
	GetPod           func(context.Context) (*corev1.Pod, error)
	ProcessExcluder  *process.Excluder
	// IfWatching allows the reconciler to only execute functions if a constraint
	// template is currently being watched. It is designed to be atomic to avoid
	// race conditions between the constraint controller and the constraint template
	// controller
	IfWatching func(schema.GroupVersionKind, func() error) (bool, error)
}

func (a *Adder) InjectCFClient(c *constraintclient.Client) {
	a.CFClient = c
}

func (a *Adder) InjectWatchManager(w *watch.Manager) {
	a.WatchManager = w
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

// Add creates a new Constraint Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.HasValidationOperations() {
		return nil
	}
	reporter, err := newStatsReporter()
	if err != nil {
		log.Error(err, "StatsReporter could not start")
		return err
	}

	r := newReconciler(mgr, a.CFClient, reporter, a.ConstraintsCache, a.Tracker)
	if a.GetPod != nil {
		r.getPod = a.GetPod
	}
	if a.IfWatching != nil {
		r.ifWatching = a.IfWatching
	}
	return add(mgr, r, a.Events)
}

type ConstraintsCache struct {
	mux   sync.RWMutex
	cache map[string]tags
}

type tags struct {
	enforcementAction util.EnforcementAction
	status            metrics.Status
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(
	mgr manager.Manager,
	cfClient *constraintclient.Client,
	reporter StatsReporter,
	constraintsCache *ConstraintsCache,
	tracker *readiness.Tracker,
) *ReconcileConstraint {
	r := &ReconcileConstraint{
		// Separate reader and writer because manager's default client bypasses the cache for unstructured resources.
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		reader:       mgr.GetCache(),

		scheme:           mgr.GetScheme(),
		cfClient:         cfClient,
		log:              log,
		reporter:         reporter,
		constraintsCache: constraintsCache,
		tracker:          tracker,
	}
	r.getPod = r.defaultGetPod
	// default
	r.ifWatching = func(_ schema.GroupVersionKind, fn func() error) (bool, error) { return true, fn() }
	return r
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler, events <-chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New("constraint-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided constraint
	err = c.Watch(
		source.Channel(
			events,
			handler.EnqueueRequestsFromMapFunc(util.EventPackerMapFunc())))
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &constraintstatusv1beta1.ConstraintPodStatus{}, handler.TypedEnqueueRequestsFromMapFunc(constraintstatus.PodStatusToConstraintMapper(true, util.EventPackerMapFunc()))))
	if err != nil {
		return err
	}

	isVapAPIEnabled, groupVersion := transform.IsVapAPIEnabled(&log)
	if isVapAPIEnabled && operations.IsAssigned(operations.Generate) {
		obj, err := vapBindingForVersion(*groupVersion)
		if err != nil {
			return err
		}
		if err = c.Watch(source.Kind(mgr.GetCache(), obj, handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return eventPackerMapFuncFromOwnerRefs()(ctx, obj)
		}))); err != nil {
			return err
		}
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcileConstraint{}

// ReconcileConstraint reconciles an arbitrary constraint object described by Kind.
type ReconcileConstraint struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme           *runtime.Scheme
	cfClient         *constraintclient.Client
	log              logr.Logger
	reporter         StatsReporter
	constraintsCache *ConstraintsCache
	tracker          *readiness.Tracker
	getPod           func(context.Context) (*corev1.Pod, error)
	// ifWatching allows us to short-circuit get requests
	// that would otherwise trigger a watch. The bool returns whether
	// the function was executed, which can be used to determine
	// whether the reconciler should infer the object has been deleted
	ifWatching func(schema.GroupVersionKind, func() error) (bool, error)
}

// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a constraint object and makes changes based on the state read
// and what is in the constraint.Spec.
func (r *ReconcileConstraint) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	gvk, unpackedRequest, err := util.UnpackRequest(request)
	if err != nil {
		// Unrecoverable, do not retry.
		// TODO(OREN) add metric
		log.Error(err, "unpacking request", "request", request)
		return reconcile.Result{}, nil
	}

	// Sanity - make sure it is a constraint resource.
	if gvk.Group != constraintstatusv1beta1.ConstraintsGroup {
		// Unrecoverable, do not retry.
		log.Error(err, "invalid constraint GroupVersion", "gvk", gvk)
		return reconcile.Result{}, nil
	}

	deleted := false
	instance := &unstructured.Unstructured{}
	instance.SetGroupVersionKind(gvk)
	executed, err := r.ifWatching(gvk, func() error {
		return r.reader.Get(ctx, unpackedRequest.NamespacedName, instance)
	})

	// if we executed a get, we can infer deletion status from the object,
	// otherwise we must assume the object has been deleted, since we are no longer
	// watching the object (which only happens if the constraint template has been deleted)
	if (executed && err != nil) || !executed {
		if err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return reconcile.Result{}, err
		}
		deleted = true
		instance = &unstructured.Unstructured{}
		instance.SetGroupVersionKind(gvk)
		instance.SetNamespace(unpackedRequest.Namespace)
		instance.SetName(unpackedRequest.Name)
	}

	deleted = deleted || !instance.GetDeletionTimestamp().IsZero()

	constraintKey := strings.Join([]string{instance.GetKind(), instance.GetName()}, "/")
	enforcementAction, err := util.GetEnforcementAction(instance.Object)
	if err != nil {
		return reconcile.Result{}, err
	}

	reportMetrics := false
	defer func() {
		if reportMetrics {
			r.constraintsCache.reportTotalConstraints(ctx, r.reporter)
		}
	}()

	if !deleted {
		r.log.Info("handling constraint update", "instance", instance)
		status, err := r.getOrCreatePodStatus(ctx, instance)
		if err != nil {
			log.Info("could not get/create pod status object", "error", err)
			return reconcile.Result{}, err
		}
		status.Status.ConstraintUID = instance.GetUID()
		status.Status.ObservedGeneration = instance.GetGeneration()
		status.Status.Errors = nil

		if c, err := r.cfClient.GetConstraint(instance); err != nil || !reflect.DeepEqual(instance, c) {
			err := util.ValidateEnforcementAction(enforcementAction, instance.Object)
			if err != nil {
				return reconcile.Result{}, r.reportErrorOnConstraintStatus(ctx, status, err, "could not validate enforcement actions")
			}

			if err := r.cacheConstraint(ctx, instance); err != nil {
				r.constraintsCache.addConstraintKey(constraintKey, tags{
					enforcementAction: enforcementAction,
					status:            metrics.ErrorStatus,
				})
				reportMetrics = true
				return reconcile.Result{}, r.reportErrorOnConstraintStatus(ctx, status, err, "could not cache constraint")
			}
			logAddition(r.log, instance, enforcementAction)
		}

		status.Status.Enforced = true
		if err = r.writer.Update(ctx, status); err != nil {
			return reconcile.Result{Requeue: true}, nil
		}

		// adding constraint to cache and sending metrics
		r.constraintsCache.addConstraintKey(constraintKey, tags{
			enforcementAction: enforcementAction,
			status:            metrics.ActiveStatus,
		})
		reportMetrics = true
		requeueAfter, err := r.manageVAPB(ctx, enforcementAction, instance, status)
		if err != nil {
			return reconcile.Result{RequeueAfter: requeueAfter}, err
		}
		if requeueAfter != time.Duration(0) {
			log.Info("requeueing after", "requeueAfter", requeueAfter)
			return reconcile.Result{RequeueAfter: requeueAfter}, nil
		}
	} else {
		r.log.Info("handling constraint delete", "instance", instance)
		r.reporter.DeleteVAPBStatus(types.NamespacedName{Name: getVAPBindingName(instance.GetName())})
		if _, err := r.cfClient.RemoveConstraint(ctx, instance); err != nil {
			if errors.Is(err, constraintclient.ErrMissingConstraint) {
				return reconcile.Result{}, err
			}
		}
		logRemoval(r.log, instance, enforcementAction)

		// cancel expectations
		t := r.tracker.For(instance.GroupVersionKind())
		t.CancelExpect(instance)

		r.constraintsCache.deleteConstraintKey(constraintKey)
		reportMetrics = true

		sName, err := constraintstatusv1beta1.KeyForConstraint(util.GetPodName(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
		statusObj := &constraintstatusv1beta1.ConstraintPodStatus{}
		statusObj.SetName(sName)
		statusObj.SetNamespace(util.GetNamespace())
		if err := r.writer.Delete(ctx, statusObj); err != nil {
			if !apierrors.IsNotFound(err) {
				return reconcile.Result{}, err
			}
		}
		isAPIEnabled, groupVersion := transform.IsVapAPIEnabled(&log)
		if isAPIEnabled {
			shouldGenerateVAPB, _, err := shouldGenerateVAPB(*DefaultGenerateVAPB, enforcementAction, instance)
			if err != nil {
				return reconcile.Result{}, err
			}
			if shouldGenerateVAPB {
				ct := &v1beta1.ConstraintTemplate{}
				err = r.reader.Get(ctx, types.NamespacedName{Name: strings.ToLower(instance.GetKind())}, ct)
				if err != nil {
					if apierrors.IsNotFound(err) {
						return reconcile.Result{}, nil
					}
					return reconcile.Result{}, err
				}
				unversionedCT := &templates.ConstraintTemplate{}
				if err := r.scheme.Convert(ct, unversionedCT, nil); err != nil {
					return reconcile.Result{}, err
				}
				hasVAP, err := ShouldGenerateVAP(unversionedCT)
				if err != nil {
					if errors.Is(err, celSchema.ErrCELEngineMissing) {
						return reconcile.Result{}, nil
					}
					return reconcile.Result{}, err
				}
				if hasVAP {
					vapBindingName := getVAPBindingName(instance.GetName())
					currentVapBinding, err := vapBindingForVersion(*groupVersion)
					if err != nil {
						return reconcile.Result{}, err
					}
					currentVapBinding.SetName(vapBindingName)
					if err := r.writer.Delete(ctx, currentVapBinding); err != nil {
						if !apierrors.IsNotFound(err) {
							return reconcile.Result{}, err
						}
					}
				}
			}
		}
	}
	return reconcile.Result{}, nil
}

func shouldGenerateVAPB(defaultGenerateVAPB bool, enforcementAction util.EnforcementAction, instance *unstructured.Unstructured) (bool, []string, error) {
	var VAPEnforcementActions []string
	var err error
	switch enforcementAction {
	case util.Scoped:
		VAPEnforcementActions, err = util.ScopedActionForEP(util.VAPEnforcementPoint, instance)
	default:
		if defaultGenerateVAPB {
			VAPEnforcementActions = []string{string(enforcementAction)}
		}
	}
	return len(VAPEnforcementActions) != 0, VAPEnforcementActions, err
}

func (r *ReconcileConstraint) defaultGetPod(_ context.Context) (*corev1.Pod, error) {
	// require injection of GetPod in order to control what client we use to
	// guarantee we don't inadvertently create a watch
	panic("GetPod must be injected to ReconcileConstraint")
}

func (r *ReconcileConstraint) getOrCreatePodStatus(ctx context.Context, constraint *unstructured.Unstructured) (*constraintstatusv1beta1.ConstraintPodStatus, error) {
	statusObj := &constraintstatusv1beta1.ConstraintPodStatus{}
	sName, err := constraintstatusv1beta1.KeyForConstraint(util.GetPodName(), constraint)
	if err != nil {
		return nil, err
	}
	key := types.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
	if err := r.reader.Get(ctx, key, statusObj); err != nil {
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
	statusObj, err = constraintstatusv1beta1.NewConstraintStatusForPod(pod, constraint, r.scheme)
	if err != nil {
		return nil, err
	}
	if err := r.writer.Create(ctx, statusObj); err != nil {
		return nil, err
	}
	return statusObj, nil
}

func ShouldGenerateVAP(ct *templates.ConstraintTemplate) (bool, error) {
	source, err := celSchema.GetSourceFromTemplate(ct)
	if err != nil {
		return false, err
	}
	if source.GenerateVAP == nil {
		return *DefaultGenerateVAP, nil
	}
	return *source.GenerateVAP, nil
}

func logAddition(l logr.Logger, constraint *unstructured.Unstructured, enforcementAction util.EnforcementAction) {
	l.Info(
		"constraint added to OPA",
		logging.EventType, "constraint_added",
		logging.ConstraintGroup, constraint.GroupVersionKind().Group,
		logging.ConstraintAPIVersion, constraint.GroupVersionKind().Version,
		logging.ConstraintKind, constraint.GetKind(),
		logging.ConstraintName, constraint.GetName(),
		logging.ConstraintAction, string(enforcementAction),
		logging.ConstraintStatus, "enforced",
	)
}

func logRemoval(l logr.Logger, constraint *unstructured.Unstructured, enforcementAction util.EnforcementAction) {
	l.Info(
		"constraint removed from OPA",
		logging.EventType, "constraint_removed",
		logging.ConstraintGroup, constraint.GroupVersionKind().Group,
		logging.ConstraintAPIVersion, constraint.GroupVersionKind().Version,
		logging.ConstraintKind, constraint.GetKind(),
		logging.ConstraintName, constraint.GetName(),
		logging.ConstraintAction, string(enforcementAction),
		logging.ConstraintStatus, "unenforced",
	)
}

func (r *ReconcileConstraint) cacheConstraint(ctx context.Context, instance *unstructured.Unstructured) error {
	t := r.tracker.For(instance.GroupVersionKind())

	obj := instance.DeepCopy()
	// Remove the status field since we do not need it
	unstructured.RemoveNestedField(obj.Object, "status")
	_, err := r.cfClient.AddConstraint(ctx, obj)
	if err != nil {
		t.TryCancelExpect(obj)
		return err
	}

	// Track for readiness
	t.Observe(instance)

	return nil
}

func (r *ReconcileConstraint) reportErrorOnConstraintStatus(ctx context.Context, status *constraintstatusv1beta1.ConstraintPodStatus, err error, message string) error {
	status.Status.Errors = append(status.Status.Errors, constraintstatusv1beta1.Error{Message: fmt.Sprintf("%s: %s", message, err)})
	if err2 := r.writer.Update(ctx, status); err2 != nil {
		log.Error(err2, message, "error", "could not update constraint status")
		return errorpkg.Wrapf(err, "%s", fmt.Sprintf("%s, could not update constraint status: %s", message, err2))
	}
	return err
}

func (r *ReconcileConstraint) manageVAPB(ctx context.Context, enforcementAction util.EnforcementAction, instance *unstructured.Unstructured, status *constraintstatusv1beta1.ConstraintPodStatus) (time.Duration, error) {
	noDelay := time.Duration(0)
	vapBindingKey := types.NamespacedName{Name: getVAPBindingName(instance.GetName())}
	if !operations.IsAssigned(operations.Generate) {
		log.Info("generate operation is not assigned, ValidatingAdmissionPolicyBinding resource will not be generated")
		r.reporter.DeleteVAPBStatus(vapBindingKey)
		return noDelay, nil
	}
	ct := &v1beta1.ConstraintTemplate{}
	err := r.reader.Get(ctx, types.NamespacedName{Name: strings.ToLower(instance.GetKind())}, ct)
	if err != nil {
		return noDelay, err
	}

	shouldGenerateVAPB, VAPEnforcementActions, err := shouldGenerateVAPB(*DefaultGenerateVAPB, enforcementAction, instance)
	if err != nil {
		log.Error(err, "could not determine if ValidatingAdmissionPolicyBinding should be generated")
		r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
		return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not determine if ValidatingAdmissionPolicyBinding should be generated")
	}
	isAPIEnabled := false
	var groupVersion *schema.GroupVersion
	if shouldGenerateVAPB {
		isAPIEnabled, groupVersion = transform.IsVapAPIEnabled(&log)
	}
	if shouldGenerateVAPB {
		if !isAPIEnabled {
			log.Error(ErrValidatingAdmissionPolicyAPIDisabled, "Cannot generate ValidatingAdmissionPolicyBinding", "constraint", instance.GetName())
			r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
			status.Status.Errors = append(status.Status.Errors, constraintstatusv1beta1.Error{Message: fmt.Sprintf("cannot generate ValidatingAdmissionPolicyBinding: %s", ErrValidatingAdmissionPolicyAPIDisabled)})
			shouldGenerateVAPB = false
		} else {
			unversionedCT := &templates.ConstraintTemplate{}
			if err := r.scheme.Convert(ct, unversionedCT, nil); err != nil {
				return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not convert ConstraintTemplate to unversioned")
			}
			hasVAP, err := ShouldGenerateVAP(unversionedCT)
			switch {
			case errors.Is(err, celSchema.ErrCELEngineMissing):
				updateEnforcementPointStatus(status, util.VAPEnforcementPoint, ErrGenerateVAPBState, err.Error(), instance.GetGeneration())
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				shouldGenerateVAPB = false
			case err != nil:
				log.Error(err, "could not determine if ConstraintTemplate is configured to generate ValidatingAdmissionPolicy", "constraint", instance.GetName(), "constraint_template", unversionedCT.GetName())
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				status.Status.Errors = append(status.Status.Errors, constraintstatusv1beta1.Error{Message: fmt.Sprintf("could not determine if ConstraintTemplate is configured to generate ValidatingAdmissionPolicy: %s", err)})
				shouldGenerateVAPB = false
			case !hasVAP:
				log.Error(ErrVAPConditionsNotSatisfied, "Cannot generate ValidatingAdmissionPolicyBinding", "constraint", instance.GetName(), "constraint_template", unversionedCT.GetName())
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				status.Status.Errors = append(status.Status.Errors, constraintstatusv1beta1.Error{Message: fmt.Sprintf("cannot generate ValidatingAdmissionPolicyBinding: %s", ErrVAPConditionsNotSatisfied)})
				shouldGenerateVAPB = false
			default:
				// reconcile for vapb generation if annotation is not set
				if ct.Annotations == nil || (ct.Annotations[BlockVAPBGenerationUntilAnnotation] == "" && ct.Annotations[VAPBGenerationAnnotation] != "unblocked") {
					return noDelay, r.reportErrorOnConstraintStatus(ctx, status, errors.New("annotation to wait for ValidatingAdmissionPolicyBinding generation not found"), "could not find annotation to wait for ValidatingAdmissionPolicyBinding generation")
				}

				if ct.Annotations[VAPBGenerationAnnotation] == "" || ct.Annotations[VAPBGenerationAnnotation] == VAPBGenerationBlocked {
					// waiting for sometime before generating vapbinding, gives api-server time to cache CRDs
					timestamp := ct.Annotations[BlockVAPBGenerationUntilAnnotation]
					t, err := time.Parse(time.RFC3339, timestamp)
					if err != nil {
						return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not parse timestamp")
					}
					if t.After(time.Now()) {
						wait := time.Until(t)
						updateEnforcementPointStatus(status, util.VAPEnforcementPoint, WaitVAPBState, fmt.Sprintf("waiting for %s before generating ValidatingAdmissionPolicyBinding to make sure api-server has cached constraint CRD", wait), instance.GetGeneration())
						return wait, r.writer.Update(ctx, status)
					}
				}
			}
		}
	}

	r.log.Info("constraint controller", "generateVAPB", shouldGenerateVAPB)
	// generate vapbinding resources
	if shouldGenerateVAPB && groupVersion != nil {
		currentVapBinding, err := vapBindingForVersion(*groupVersion)
		if err != nil {
			return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not get ValidatingAdmissionPolicyBinding API version")
		}
		vapBindingName := getVAPBindingName(instance.GetName())
		log.Info("check if vapbinding exists", "vapBindingName", vapBindingName)
		if err := r.reader.Get(ctx, types.NamespacedName{Name: vapBindingName}, currentVapBinding); err != nil {
			if !apierrors.IsNotFound(err) {
				return noDelay, err
			}
			currentVapBinding = nil
		}
		transformedVapBinding, err := transform.ConstraintToBinding(instance, VAPEnforcementActions)
		if err != nil {
			return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not transform constraint to ValidatingAdmissionPolicyBinding")
		}

		newVapBinding, err := getRunTimeVAPBinding(groupVersion, transformedVapBinding, currentVapBinding)
		if err != nil {
			return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not get ValidatingAdmissionPolicyBinding object with runtime group version")
		}

		if err := controllerutil.SetControllerReference(instance, newVapBinding, r.scheme); err != nil {
			return noDelay, err
		}

		if currentVapBinding == nil {
			log.Info("creating vapbinding")
			if err := r.writer.Create(ctx, newVapBinding); err != nil {
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, fmt.Sprintf("could not create ValidatingAdmissionPolicyBinding: %s", vapBindingName))
			}
		} else if !reflect.DeepEqual(currentVapBinding, newVapBinding) {
			log.Info("updating vapbinding")
			if err := r.writer.Update(ctx, newVapBinding); err != nil {
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, fmt.Sprintf("could not update ValidatingAdmissionPolicyBinding: %s", vapBindingName))
			}
		}
		updateEnforcementPointStatus(status, util.VAPEnforcementPoint, GeneratedVAPBState, "", instance.GetGeneration())
		r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusActive)
	}
	// do not generate vapbinding resources
	// remove if exists
	if !shouldGenerateVAPB && groupVersion != nil {
		currentVapBinding, err := vapBindingForVersion(*groupVersion)
		if err != nil {
			return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, "could not get ValidatingAdmissionPolicyBinding API version")
		}
		vapBindingName := getVAPBindingName(instance.GetName())
		log.Info("check if vapbinding exists", "vapBindingName", vapBindingName)
		if err := r.reader.Get(ctx, types.NamespacedName{Name: vapBindingName}, currentVapBinding); err != nil {
			if !apierrors.IsNotFound(err) && !errors.As(err, &discoveryErr) && !meta.IsNoMatchError(err) {
				return noDelay, err
			}
			currentVapBinding = nil
		}
		if currentVapBinding != nil {
			log.Info("deleting vapbinding")
			if err := r.writer.Delete(ctx, currentVapBinding); err != nil {
				r.reporter.ReportVAPBStatus(vapBindingKey, metrics.VAPStatusError)
				return noDelay, r.reportErrorOnConstraintStatus(ctx, status, err, fmt.Sprintf("could not delete ValidatingAdmissionPolicyBinding: %s", vapBindingName))
			}
			cleanEnforcementPointStatus(status, util.VAPEnforcementPoint)
		}
		r.reporter.DeleteVAPBStatus(vapBindingKey)
	}
	return noDelay, r.writer.Update(ctx, status)
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

func (c *ConstraintsCache) reportTotalConstraints(ctx context.Context, reporter StatsReporter) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	totals := make(map[tags]int)
	// report total number of constraints
	for _, v := range c.cache {
		totals[v]++
	}

	for _, enforcementAction := range util.KnownEnforcementActions {
		for _, status := range metrics.AllStatuses {
			t := tags{
				enforcementAction: enforcementAction,
				status:            status,
			}
			if err := reporter.reportConstraints(ctx, t, int64(totals[t])); err != nil {
				log.Error(err, "failed to report total constraints")
			}
		}
	}
}

func vapBindingForVersion(gvk schema.GroupVersion) (client.Object, error) {
	switch gvk.Version {
	case "v1":
		return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}, nil
	case "v1beta1":
		return &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}, nil
	default:
		return nil, errors.New("unrecognized version")
	}
}

func getRunTimeVAPBinding(gvk *schema.GroupVersion, transformedVapBinding *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding, currentVapBinding client.Object) (client.Object, error) {
	if currentVapBinding == nil {
		if gvk.Version == "v1" {
			return v1beta1ToV1(transformedVapBinding)
		}
		return transformedVapBinding.DeepCopy(), nil
	}
	if gvk.Version == "v1" {
		v1CurrentVAPBinding, ok := currentVapBinding.(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
		if !ok {
			return nil, errors.New("unable to convert to v1 VAP")
		}
		v1CurrentVAPBinding = v1CurrentVAPBinding.DeepCopy()
		tempVAPBinding, err := v1beta1ToV1(transformedVapBinding)
		if err != nil {
			return nil, err
		}
		v1CurrentVAPBinding.Spec = tempVAPBinding.Spec
		return v1CurrentVAPBinding, nil
	}
	v1beta1VAPBinding, ok := currentVapBinding.(*admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding)
	if !ok {
		return nil, errors.New("unable to convert to v1beta1 VAP")
	}
	v1beta1VAPBinding.Spec = transformedVapBinding.Spec
	return v1beta1VAPBinding.DeepCopy(), nil
}

func v1beta1ToV1(v1beta1Obj *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding) (*admissionregistrationv1.ValidatingAdmissionPolicyBinding, error) {
	// TODO(jgabani): Use r.scheme.Convert to convert from v1beta1 to v1 once the conversion bug is fixed - https://github.com/kubernetes/kubernetes/issues/126582
	obj := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	obj.SetName(v1beta1Obj.GetName())
	obj.Spec.PolicyName = v1beta1Obj.Spec.PolicyName
	obj.Spec.ParamRef = &admissionregistrationv1.ParamRef{
		Name:                    v1beta1Obj.Spec.ParamRef.Name,
		ParameterNotFoundAction: ptr.To[admissionregistrationv1.ParameterNotFoundActionType](admissionregistrationv1.AllowAction),
	}
	actions := []admissionregistrationv1.ValidationAction{}

	for _, action := range v1beta1Obj.Spec.ValidationActions {
		switch action {
		case admissionregistrationv1beta1.Deny:
			actions = append(actions, admissionregistrationv1.Deny)
		case admissionregistrationv1beta1.Warn:
			actions = append(actions, admissionregistrationv1.Warn)
		case admissionregistrationv1beta1.Audit:
			actions = append(actions, admissionregistrationv1.Audit)
		default:
			return nil, fmt.Errorf("%w: unrecognized enforcement action %s, must be `warn`, `deny` or `dryrun`", transform.ErrBadEnforcementAction, action)
		}
	}

	obj.Spec.ValidationActions = actions
	if v1beta1Obj.Spec.MatchResources != nil {
		obj.Spec.MatchResources = &admissionregistrationv1.MatchResources{}
		if v1beta1Obj.Spec.MatchResources.ObjectSelector != nil {
			obj.Spec.MatchResources.ObjectSelector = v1beta1Obj.Spec.MatchResources.ObjectSelector
		}
		if v1beta1Obj.Spec.MatchResources.NamespaceSelector != nil {
			obj.Spec.MatchResources.NamespaceSelector = v1beta1Obj.Spec.MatchResources.NamespaceSelector
		}
	}

	return obj, nil
}

func updateEnforcementPointStatus(status *constraintstatusv1beta1.ConstraintPodStatus, enforcementPoint string, state string, message string, observedGeneration int64) {
	enforcementPointStatus := constraintstatusv1beta1.EnforcementPointStatus{EnforcementPoint: enforcementPoint, State: state, ObservedGeneration: observedGeneration, Message: message}
	for i, ep := range status.Status.EnforcementPointsStatus {
		if ep.EnforcementPoint == enforcementPoint {
			status.Status.EnforcementPointsStatus[i] = enforcementPointStatus
			return
		}
	}
	status.Status.EnforcementPointsStatus = append(status.Status.EnforcementPointsStatus, enforcementPointStatus)
}

func cleanEnforcementPointStatus(status *constraintstatusv1beta1.ConstraintPodStatus, enforcementPoint string) {
	for i, ep := range status.Status.EnforcementPointsStatus {
		if ep.EnforcementPoint == enforcementPoint {
			status.Status.EnforcementPointsStatus = append(status.Status.EnforcementPointsStatus[:i], status.Status.EnforcementPointsStatus[i+1:]...)
			return
		}
	}
}

func getVAPBindingName(constraintName string) string {
	return fmt.Sprintf("gatekeeper-%s", constraintName)
}

func eventPackerMapFuncFromOwnerRefs() handler.MapFunc {
	mf := util.EventPackerMapFunc()
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		var out []reconcile.Request
		for _, owner := range o.GetOwnerReferences() {
			if owner.Controller != nil && *owner.Controller && strings.HasPrefix(owner.APIVersion, "constraints.gatekeeper.sh/") {
				// APIVersion may be "group/version"; split into group and version
				group := ""
				version := owner.APIVersion
				if parts := strings.SplitN(owner.APIVersion, "/", 2); len(parts) == 2 {
					group = parts[0]
					version = parts[1]
				}
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: owner.Kind})
				u.SetName(owner.Name)
				out = append(out, mf(ctx, u)...)
			}
		}
		return out
	}
}
