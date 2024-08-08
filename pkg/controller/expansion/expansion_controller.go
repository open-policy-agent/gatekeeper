package expansion

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	expansionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues("kind", "ExpansionTemplate", logging.Process, "template_expansion_controller")

// eventQueueSize is how many events to queue before blocking.
const eventQueueSize = 1024

type Adder struct {
	WatchManager    *watch.Manager
	ExpansionSystem *expansion.System
	Tracker         *readiness.Tracker
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(context.Context) (*corev1.Pod, error)
}

func (a *Adder) Add(mgr manager.Manager) error {
	if !*expansion.ExpansionEnabled {
		return nil
	}
	r := newReconciler(mgr, a.ExpansionSystem, a.GetPod, a.Tracker)
	return add(mgr, r)
}

func (a *Adder) InjectControllerSwitch(_ *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(tracker *readiness.Tracker) {
	a.Tracker = tracker
}

func (a *Adder) InjectExpansionSystem(expansionSystem *expansion.System) {
	a.ExpansionSystem = expansionSystem
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

type Reconciler struct {
	client.Client
	system       *expansion.System
	scheme       *runtime.Scheme
	registry     *etRegistry
	statusClient client.StatusClient
	tracker      *readiness.Tracker
	events       chan event.GenericEvent
	eventSource  source.Source

	getPod func(context.Context) (*corev1.Pod, error)
}

func newReconciler(mgr manager.Manager,
	system *expansion.System,
	getPod func(ctx context.Context) (*corev1.Pod, error),
	tracker *readiness.Tracker,
) *Reconciler {
	ev := make(chan event.GenericEvent, eventQueueSize)
	return &Reconciler{
		Client:       mgr.GetClient(),
		system:       system,
		scheme:       mgr.GetScheme(),
		registry:     newRegistry(),
		statusClient: mgr.GetClient(),
		getPod:       getPod,
		tracker:      tracker,
		events:       ev,
		eventSource:  source.Channel(ev, &handler.EnqueueRequestForObject{}),
	}
}

func add(mgr manager.Manager, r *Reconciler) error {
	c, err := controller.New("expansion-template-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for enqueued events
	if r.eventSource != nil {
		err = c.Watch(r.eventSource)
		if err != nil {
			return err
		}
	}

	// Watch for changes to ExpansionTemplates
	return c.Watch(
		source.Kind(mgr.GetCache(), &expansionv1beta1.ExpansionTemplate{},
			&handler.TypedEnqueueRequestForObject[*expansionv1beta1.ExpansionTemplate]{}))
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	defer r.registry.report(ctx)
	log.V(logging.DebugLevel).Info("Reconcile", "request", request, "namespace", request.Namespace, "name", request.Name)

	deleted := false
	versionedET := &expansionv1beta1.ExpansionTemplate{}
	err := r.Get(ctx, request.NamespacedName, versionedET)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	et := &unversioned.ExpansionTemplate{}
	if err := r.scheme.Convert(versionedET, et, nil); err != nil {
		return reconcile.Result{}, err
	}
	oldConflicts := r.system.GetConflicts()

	if !et.GetDeletionTimestamp().IsZero() {
		deleted = true
	}

	if deleted {
		// et will be an empty struct. We set the metadata name, which is
		// used as a key to delete it from the expansion system
		et.Name = request.Name
		if err := r.system.RemoveTemplate(et); err != nil {
			r.getTracker().TryCancelExpect(versionedET)
			return reconcile.Result{}, err
		}
		log.V(logging.DebugLevel).Info("removed expansion template", "template name", et.GetName())
		r.registry.remove(request.NamespacedName)
		r.getTracker().CancelExpect(versionedET)
		r.queueConflicts(oldConflicts)
		return reconcile.Result{}, r.deleteStatus(ctx, request.NamespacedName.Name)
	}

	upsertErr := r.system.UpsertTemplate(et)
	if upsertErr == nil {
		r.getTracker().Observe(versionedET)
		r.registry.add(request.NamespacedName, metrics.ActiveStatus)
	} else {
		r.getTracker().TryCancelExpect(versionedET)
		r.registry.add(request.NamespacedName, metrics.ErrorStatus)
		log.Error(upsertErr, "upserting template", "template_name", et.GetName())
	}

	r.queueConflicts(oldConflicts)
	return reconcile.Result{}, r.updateOrCreatePodStatus(ctx, et, upsertErr)
}

func (r *Reconciler) queueConflicts(old expansion.IDSet) {
	for tID := range symmetricDiff(old, r.system.GetConflicts()) {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(expansionv1beta1.GroupVersion.WithKind("ExpansionTemplate"))
		// ExpansionTemplate is cluster-scoped, so we do not set namespace
		u.SetName(string(tID))

		r.events <- event.GenericEvent{Object: u}
	}
}

func symmetricDiff(x, y expansion.IDSet) expansion.IDSet {
	sDiff := make(expansion.IDSet)

	for id := range x {
		if _, exists := y[id]; !exists {
			sDiff[id] = true
		}
	}
	for id := range y {
		if _, exists := x[id]; !exists {
			sDiff[id] = true
		}
	}

	return sDiff
}

func (r *Reconciler) deleteStatus(ctx context.Context, etName string) error {
	status := &statusv1beta1.ExpansionTemplatePodStatus{}
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := statusv1beta1.KeyForExpansionTemplate(pod.Name, etName)
	if err != nil {
		return fmt.Errorf("getting key for expansiontemplate: %w", err)
	}
	status.SetName(sName)
	status.SetNamespace(util.GetNamespace())
	if err := r.Delete(ctx, status); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *Reconciler) updateOrCreatePodStatus(ctx context.Context, et *unversioned.ExpansionTemplate, etErr error) error {
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}

	// Check if it exists already
	sNS := pod.Namespace
	sName, err := statusv1beta1.KeyForExpansionTemplate(pod.Name, et.GetName())
	if err != nil {
		return fmt.Errorf("getting key for expansiontemplate: %w", err)
	}
	shouldCreate := true
	status := &statusv1beta1.ExpansionTemplatePodStatus{}

	err = r.Get(ctx, types.NamespacedName{Namespace: sNS, Name: sName}, status)
	switch {
	case err == nil:
		shouldCreate = false
	case apierrors.IsNotFound(err):
		if status, err = r.newETStatus(pod, et); err != nil {
			return fmt.Errorf("creating new expansiontemplate status: %w", err)
		}
	default:
		return fmt.Errorf("getting expansion status in name %s, namespace %s: %w", et.GetName(), et.GetNamespace(), err)
	}

	setStatusError(status, etErr)
	status.Status.ObservedGeneration = et.GetGeneration()

	if shouldCreate {
		return r.Create(ctx, status)
	}
	return r.Update(ctx, status)
}

func (r *Reconciler) newETStatus(pod *corev1.Pod, et *unversioned.ExpansionTemplate) (*statusv1beta1.ExpansionTemplatePodStatus, error) {
	status, err := statusv1beta1.NewExpansionTemplateStatusForPod(pod, et.GetName(), r.scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	status.Status.TemplateUID = et.GetUID()

	return status, nil
}

func (r *Reconciler) getTracker() readiness.Expectations {
	return r.tracker.For(expansionv1beta1.GroupVersion.WithKind("ExpansionTemplate"))
}

func setStatusError(status *statusv1beta1.ExpansionTemplatePodStatus, etErr error) {
	if etErr == nil {
		status.Status.Errors = nil
		return
	}

	e := &statusv1beta1.ExpansionTemplateError{Message: etErr.Error()}
	status.Status.Errors = []*statusv1beta1.ExpansionTemplateError{e}
}
