package expansion

import (
	"context"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/apis/expansion/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues("kind", "TemplateExpansion", logging.Process, "template_expansion_controller")

type Adder struct {
	WatchManager    *watch.Manager
	ExpansionSystem *expansion.System
}

func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.ExpansionSystem)
	return add(mgr, r)
}

func (a *Adder) InjectOpa(o *constraintclient.Client) {}

func (a *Adder) InjectWatchManager(w *watch.Manager) {}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(t *readiness.Tracker) {}

func (a *Adder) InjectMutationSystem(mutationSystem *mutation.System) {}

func (a *Adder) InjectExpansionSystem(expansionSystem *expansion.System) {
	a.ExpansionSystem = expansionSystem
}

func (a *Adder) InjectProviderCache(providerCache *externaldata.ProviderCache) {}

type Reconciler struct {
	client.Client
	system *expansion.System
	scheme *runtime.Scheme
}

func newReconciler(mgr manager.Manager, system *expansion.System) *Reconciler {
	return &Reconciler{Client: mgr.GetClient(), system: system, scheme: mgr.GetScheme()}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("template-expansion-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	return c.Watch(
		&source.Kind{Type: &v1alpha1.TemplateExpansion{}},
		&handler.EnqueueRequestForObject{})
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile", "request", request, "namespace", request.Namespace, "name", request.Name)

	deleted := false
	te := &v1alpha1.TemplateExpansion{}
	err := r.Get(ctx, request.NamespacedName, te)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	unversionedTE := &unversioned.TemplateExpansion{}
	if err := r.scheme.Convert(te, unversionedTE, nil); err != nil {
		return reconcile.Result{}, err
	}
	if deleted {
		// unversionedTE will be an empty struct. We set the metadata name, which is
		// used as a key to delete it from the expansion system
		unversionedTE.ObjectMeta.Name = request.Name
		if err := r.system.RemoveTemplate(unversionedTE); err != nil {
			return reconcile.Result{}, err
		}
		log.Info("removed template expansion", "template name", unversionedTE.ObjectMeta.Name)
	} else {
		if err := r.system.UpsertTemplate(unversionedTE); err != nil {
			return reconcile.Result{}, err
		}
		log.Info("upserted template expansion", "template name", unversionedTE.ObjectMeta.Name)
	}

	return reconcile.Result{}, nil
}
