package expansion

import (
	"context"
	"fmt"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues("kind", "ExpansionTemplate", logging.Process, "template_expansion_controller")

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

func (a *Adder) InjectOpa(_ *constraintclient.Client) {}

func (a *Adder) InjectWatchManager(_ *watch.Manager) {}

func (a *Adder) InjectControllerSwitch(_ *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(tracker *readiness.Tracker) {
	a.Tracker = tracker
}

func (a *Adder) InjectMutationSystem(_ *mutation.System) {}

func (a *Adder) InjectExpansionSystem(expansionSystem *expansion.System) {
	a.ExpansionSystem = expansionSystem
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

func (a *Adder) InjectProviderCache(_ *externaldata.ProviderCache) {}

type Reconciler struct {
	client.Client
	system       *expansion.System
	scheme       *runtime.Scheme
	registry     *etRegistry
	statusClient client.StatusClient
	tracker      *readiness.Tracker

	getPod func(context.Context) (*corev1.Pod, error)
}

func newReconciler(mgr manager.Manager, system *expansion.System, getPod func(ctx context.Context) (*corev1.Pod, error), tracker *readiness.Tracker) *Reconciler {
	return &Reconciler{
		Client:       mgr.GetClient(),
		system:       system,
		scheme:       mgr.GetScheme(),
		registry:     newRegistry(),
		statusClient: mgr.GetClient(),
		getPod:       getPod,
		tracker:      tracker,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("expansion-template-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	return c.Watch(
		&source.Kind{Type: &v1alpha1.ExpansionTemplate{}},
		&handler.EnqueueRequestForObject{})
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	defer r.registry.report(ctx)
	log.Info("Reconcile", "request", request, "namespace", request.Namespace, "name", request.Name)

	deleted := false
	versionedET := &v1alpha1.ExpansionTemplate{}
	err := r.Get(ctx, request.NamespacedName, versionedET)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	et := &unversioned.ExpansionTemplate{}
	if err := r.scheme.Convert(versionedET, et, nil); err != nil {
		return reconcile.Result{}, err
	}

	if deleted {
		// et will be an empty struct. We set the metadata name, which is
		// used as a key to delete it from the expansion system
		et.Name = request.Name
		if err := r.system.RemoveTemplate(et); err != nil {
			r.getTracker().TryCancelExpect(versionedET)
			return reconcile.Result{}, err
		}
		log.Info("removed expansion template", "template name", et.GetName())
		r.registry.remove(request.NamespacedName)
		r.getTracker().CancelExpect(versionedET)
		return reconcile.Result{}, r.deleteStatus(ctx, request.NamespacedName.Name)
	}

	upsertErr := r.system.UpsertTemplate(et)
	if upsertErr == nil {
		log.Info("[readiness] observed ExpansionTemplate", "template name", et.GetName())
		r.getTracker().Observe(versionedET)
		r.registry.add(request.NamespacedName, metrics.ActiveStatus)
	} else {
		r.getTracker().TryCancelExpect(versionedET)
		r.registry.add(request.NamespacedName, metrics.ErrorStatus)
		log.Error(upsertErr, "upserting template", "template_name", et.GetName())
	}

	return reconcile.Result{}, r.updateOrCreatePodStatus(ctx, et, upsertErr)
}

func (r *Reconciler) deleteStatus(ctx context.Context, etName string) error {
	status := &v1beta1.ExpansionTemplatePodStatus{}
	pod, err := r.getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := v1beta1.KeyForExpansionTemplate(pod.Name, etName)
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
	sName, err := v1beta1.KeyForExpansionTemplate(pod.Name, et.GetName())
	if err != nil {
		return fmt.Errorf("getting key for expansiontemplate: %w", err)
	}
	shouldCreate := true
	status := &v1beta1.ExpansionTemplatePodStatus{}

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

func (r *Reconciler) newETStatus(pod *corev1.Pod, et *unversioned.ExpansionTemplate) (*v1beta1.ExpansionTemplatePodStatus, error) {
	status, err := statusv1beta1.NewExpansionTemplateStatusForPod(pod, et.GetName(), r.scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	status.Status.TemplateUID = et.GetUID()

	return status, nil
}

func (r *Reconciler) getTracker() readiness.Expectations {
	return r.tracker.For(v1alpha1.GroupVersion.WithKind("ExpansionTemplate"))
}

func setStatusError(status *v1beta1.ExpansionTemplatePodStatus, etErr error) {
	if etErr == nil {
		status.Status.Errors = nil
		return
	}

	e := &v1beta1.ExpansionTemplateError{Message: etErr.Error()}
	status.Status.Errors = append(status.Status.Errors, e)
}
