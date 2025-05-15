package export

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	ExportEnabled = flag.Bool("enable-violation-export", false, "(alpha) Enable exporting violations to external systems")
	log           = logf.Log.WithName("controller").WithValues(logging.Process, "export_controller")
)

type Adder struct {
	ExportSystem *export.System
}

func (a *Adder) Add(mgr manager.Manager) error {
	if !*ExportEnabled {
		return nil
	}
	log.Info("Warning: Alpha flag enable-violation-export is set to true. This flag may change in the future.")
	r := newReconciler(mgr, a.ExportSystem)
	return add(mgr, r)
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

func (a *Adder) InjectExportSystem(exportSystem *export.System) {
	a.ExportSystem = exportSystem
}

type Reconciler struct {
	client.Client
	scheme *runtime.Scheme
	system *export.System
}

func newReconciler(mgr manager.Manager, system *export.System) *Reconciler {
	return &Reconciler{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		system: system,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("export-config-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	return c.Watch(
		source.Kind(mgr.GetCache(), &corev1.ConfigMap{},
			&handler.TypedEnqueueRequestForObject[*corev1.ConfigMap]{},
			predicate.TypedFuncs[*corev1.ConfigMap]{
				CreateFunc: func(e event.TypedCreateEvent[*corev1.ConfigMap]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*corev1.ConfigMap]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace()
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*corev1.ConfigMap]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				GenericFunc: func(e event.TypedGenericEvent[*corev1.ConfigMap]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
			},
		))
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile", "request", request, "namespace", request.Namespace, "name", request.Name)

	deleted := false
	cfg := &corev1.ConfigMap{}
	err := r.Get(ctx, request.NamespacedName, cfg)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	if deleted {
		err := r.system.CloseConnection(request.Name)
		if err != nil {
			return reconcile.Result{Requeue: true}, err
		}
		log.Info("removed connection", "name", request.Name)
		return reconcile.Result{}, nil
	}

	if len(cfg.Data) == 0 {
		return reconcile.Result{}, fmt.Errorf("data missing in configmap %s, unable to configure exporter", request.NamespacedName)
	}
	if _, ok := cfg.Data["driver"]; !ok {
		return reconcile.Result{}, fmt.Errorf("missing driver field in configmap %s, unable to configure exporter", request.NamespacedName)
	}
	var config interface{}
	err = json.Unmarshal([]byte(cfg.Data["config"]), &config)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.system.UpsertConnection(ctx, config, request.Name, strings.ToLower(cfg.Data["driver"]))
	if err != nil {
		return reconcile.Result{}, err
	}

	log.Info("Connection upsert successful", "name", request.Name, "driver", cfg.Data["driver"])
	return reconcile.Result{}, nil
}
