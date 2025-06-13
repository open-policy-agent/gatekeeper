package export

import (
	"context"
	"flag"
	"fmt"

	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/connectionstatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

const (
	defaultConnection = "audit-connection"
	defaultChannel    = "audit-channel"
)

var (
	ExportEnabled   = flag.Bool("enable-violation-export", false, "(alpha) Enable exporting violations to external systems")
	AuditConnection = flag.String("audit-connection", defaultConnection, "(alpha) Connection name for exporting audit violation messages. Defaults to audit-connection")
	AuditChannel    = flag.String("audit-channel", defaultChannel, "(alpha) Channel name for exporting audit violation messages. Defaults to audit-channel")
	log             = logf.Log.WithName("controller").WithValues(logging.Process, "export_controller")
)

type Adder struct {
	ExportSystem export.Exporter
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(context.Context) (*corev1.Pod, error)
}

func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.ExportSystem, a.GetPod)
	if r == nil {
		log.Info("Export functionality is disabled, skipping export connection controller setup")
		return nil
	}
	return add(mgr, r)
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

func (a *Adder) InjectExportSystem(exportSystem export.Exporter) {
	a.ExportSystem = exportSystem
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

type Reconciler struct {
	reader client.Reader
	writer client.Writer
	scheme *runtime.Scheme
	system export.Exporter
	getPod func(context.Context) (*corev1.Pod, error)
}

func newReconciler(mgr manager.Manager, system export.Exporter, getPod func(context.Context) (*corev1.Pod, error)) *Reconciler {
	if !*ExportEnabled {
		log.Info("Export is disabled via flag")
		return nil
	}

	log.Info("Warning: Alpha flag enable-violation-export is set to true. This flag may change in the future.")

	return &Reconciler{
		reader: mgr.GetCache(),
		writer: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		system: system,
		getPod: getPod,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("export-connection-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &connectionv1alpha1.Connection{},
			&handler.TypedEnqueueRequestForObject[*connectionv1alpha1.Connection]{},
			predicate.TypedFuncs[*connectionv1alpha1.Connection]{
				CreateFunc: func(e event.TypedCreateEvent[*connectionv1alpha1.Connection]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*connectionv1alpha1.Connection]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace()
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*connectionv1alpha1.Connection]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				GenericFunc: func(e event.TypedGenericEvent[*connectionv1alpha1.Connection]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
			},
		),
	)
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &statusv1beta1.ConnectionPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(connectionstatus.PodStatusToConnectionMapper(true)),
			predicate.TypedFuncs[*statusv1beta1.ConnectionPodStatus]{
				CreateFunc: func(e event.TypedCreateEvent[*statusv1beta1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*statusv1beta1.ConnectionPodStatus]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace()
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*statusv1beta1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				GenericFunc: func(e event.TypedGenericEvent[*statusv1beta1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
			},
		),
	)
	if err != nil {
		return err
	}

	return nil
}

// +kubebuilder:rbac:groups=connection.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile request", "namespace", request.Namespace, "name", request.Name)

	if request.Name != *AuditConnection {
		msg := fmt.Sprintf("Ignoring unsupported connection name %s. Connection name should align with flag --audit-connection set or defaulted to %s", request.Name, *AuditConnection)
		log.Info(msg, "namespace", request.Namespace)
		return reconcile.Result{}, nil
	}

	deleted := false
	connObj := &connectionv1alpha1.Connection{}
	err := r.reader.Get(ctx, request.NamespacedName, connObj)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
	}

	if deleted {
		err := r.system.CloseConnection(request.Name)
		if err != nil {
			return reconcile.Result{Requeue: true}, deleteStatus(ctx, r.writer, request.Namespace, request.Name, r.getPod)
		}
		log.Info("removed connection", "name", request.Name)
		return reconcile.Result{}, deleteStatus(ctx, r.writer, request.Namespace, request.Name, r.getPod)
	}

	err = r.system.UpsertConnection(ctx, connObj.Spec.Config.Value, request.Name, connObj.Spec.Driver)
	if err != nil {
		return reconcile.Result{Requeue: true}, updateOrCreateConnectionPodStatus(ctx, r.reader, r.writer, r.scheme, connObj, []*statusv1beta1.ConnectionError{{Type: statusv1beta1.UpsertConnectionError, Message: err.Error()}}, false, r.getPod)
	}

	log.Info("Connection upsert successful", "name", request.Name, "driver", connObj.Spec.Driver)
	return reconcile.Result{}, updateOrCreateConnectionPodStatus(ctx, r.reader, r.writer, r.scheme, connObj, []*statusv1beta1.ConnectionError{}, false, r.getPod)
}

func UpdateOrCreateConnectionPodStatus(
	ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	scheme *runtime.Scheme,
	connObjName string,
	exportErrors []*statusv1beta1.ConnectionError,
	activeConnection bool,
	getPod func(context.Context) (*corev1.Pod, error)) error {

	// Since the caller from Audit won't have an incoming request
	// use the connection name from the audit connection flag as the predetermined connection name
	request := types.NamespacedName{
		Namespace: util.GetNamespace(),
		Name:      connObjName,
	}
	connObj := &connectionv1alpha1.Connection{}
	err := reader.Get(ctx, request, connObj)
	if err != nil {
		return err
	}
	return updateOrCreateConnectionPodStatus(ctx, reader, writer, scheme, connObj, exportErrors, activeConnection, getPod)
}

func updateOrCreateConnectionPodStatus(ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	scheme *runtime.Scheme,
	connObj *connectionv1alpha1.Connection,
	exportErrors []*statusv1beta1.ConnectionError,
	activeConnection bool,
	getPod func(context.Context) (*corev1.Pod, error)) error {

	pod, err := getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}

	// Check if it exists already
	statusNS := pod.Namespace
	statusName, err := statusv1beta1.KeyForConnection(pod.Name, connObj.GetNamespace(), connObj.GetName())
	if err != nil {
		return fmt.Errorf("getting key for connection: %w", err)
	}
	shouldCreate := true
	connPodStatusObj := &statusv1beta1.ConnectionPodStatus{}

	err = reader.Get(ctx, types.NamespacedName{Namespace: statusNS, Name: statusName}, connPodStatusObj)
	switch {
	case err == nil:
		shouldCreate = false
	case apierrors.IsNotFound(err):
		if connPodStatusObj, err = newConnectionPodStatus(scheme, pod, connObj); err != nil {
			return fmt.Errorf("creating new connection connPodStatusObj: %w", err)
		}
	default:
		return fmt.Errorf("getting connection object status in name %s, namespace %s: %w", connObj.GetName(), connObj.GetNamespace(), err)
	}

	// ObservedGeneration is used to track the generation of the connection object
	connPodStatusObj.Status.ObservedGeneration = connObj.GetGeneration()

	// Let the caller decide if the connection is active depending on if it's publishing or upserting
	connPodStatusObj.Status.Active = activeConnection

	setStatusErrors(connPodStatusObj, exportErrors)

	if shouldCreate {
		return writer.Create(ctx, connPodStatusObj)
	}
	return writer.Update(ctx, connPodStatusObj)
}

func deleteStatus(ctx context.Context,
	writer client.Writer,
	connectionNamespace string,
	connectionName string,
	getPod func(context.Context) (*corev1.Pod, error)) error {
	connPodStatusObj := &statusv1beta1.ConnectionPodStatus{}
	pod, err := getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := statusv1beta1.KeyForConnection(pod.Name, connectionNamespace, connectionName)
	if err != nil {
		return fmt.Errorf("getting key for connection: %w", err)
	}
	connPodStatusObj.SetName(sName)
	connPodStatusObj.SetNamespace(util.GetNamespace())
	if err := writer.Delete(ctx, connPodStatusObj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func newConnectionPodStatus(scheme *runtime.Scheme,
	pod *corev1.Pod,
	connObj *connectionv1alpha1.Connection) (*statusv1beta1.ConnectionPodStatus, error) {
	connPodStatusObj, err := statusv1beta1.NewConnectionStatusForPod(pod, connObj.GetNamespace(), connObj.GetName(), scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	connPodStatusObj.Status.ConnectionUID = connObj.GetUID()

	return connPodStatusObj, nil
}

func setStatusErrors(
	connPodStatusObj *statusv1beta1.ConnectionPodStatus,
	exportErrors []*statusv1beta1.ConnectionError) {
	if len(exportErrors) == 0 {
		connPodStatusObj.Status.Errors = nil
		return
	}
	connPodStatusObj.Status.Errors = exportErrors
}
