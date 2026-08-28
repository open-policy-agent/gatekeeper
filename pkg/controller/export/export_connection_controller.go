package export

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/connectionstatus"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues(logging.Process, "export_controller")

type Adder struct {
	ExportSystem export.Exporter
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(context.Context) (*corev1.Pod, error)
}

func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.ExportSystem, *exportutil.AuditConnection, a.GetPod)
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
	// TODO: Refactor this once multiple connections are supported, for now this helps with injecting dependency for tests
	auditConnectionName string
	getPod              func(context.Context) (*corev1.Pod, error)
}

func newReconciler(mgr manager.Manager, system export.Exporter, auditConnectionName string, getPod func(context.Context) (*corev1.Pod, error)) *Reconciler {
	if !*exportutil.ExportEnabled && !*exportutil.AdmissionExportEnabled {
		log.Info("Export is disabled via flag")
		return nil
	}

	log.Info("Warning: Alpha violation export is enabled. This feature may change in the future.")

	return &Reconciler{
		reader:              mgr.GetCache(),
		writer:              mgr.GetClient(),
		scheme:              mgr.GetScheme(),
		system:              system,
		auditConnectionName: auditConnectionName,
		getPod:              getPod,
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
			mgr.GetCache(), &statusv1alpha1.ConnectionPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(connectionstatus.PodStatusToConnectionMapper(true)),
			predicate.TypedFuncs[*statusv1alpha1.ConnectionPodStatus]{
				CreateFunc: func(e event.TypedCreateEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace() && connectionPodStatusUpdateRequiresReconcile(e.ObjectOld, e.ObjectNew)
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				GenericFunc: func(e event.TypedGenericEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
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

// connectionPodStatusUpdateRequiresReconcile filters publisher-owned health
// updates. They must still reach the status aggregator, but cannot change the
// configured export driver and should not cause an UpsertConnection feedback loop.
func connectionPodStatusUpdateRequiresReconcile(oldStatus, newStatus *statusv1alpha1.ConnectionPodStatus) bool {
	if oldStatus == nil || newStatus == nil || !reflect.DeepEqual(oldStatus.GetLabels(), newStatus.GetLabels()) {
		return true
	}
	oldConnectionStatus := oldStatus.Status
	newConnectionStatus := newStatus.Status
	oldConnectionStatus.PublishStatuses = nil
	newConnectionStatus.PublishStatuses = nil
	return !reflect.DeepEqual(oldConnectionStatus, newConnectionStatus)
}

// +kubebuilder:rbac:groups=connection.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile request", "namespace", request.Namespace, "name", request.Name)

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
			log.Error(err, "failed to close connection", "name", request.Name)
			return reconcile.Result{Requeue: true}, deleteStatus(ctx, r.writer, request.Namespace, request.Name, r.getPod)
		}
		log.Info("removed connection", "name", request.Name)
		return reconcile.Result{}, deleteStatus(ctx, r.writer, request.Namespace, request.Name, r.getPod)
	}
	if request.Name != r.auditConnectionName {
		err := fmt.Errorf("error unsupported connection name %s. Connection name should align with flag --audit-connection set or defaulted to '%s'", request.Name, r.auditConnectionName)
		log.Error(err, "unsupported connection", "namespace", request.Namespace)
		connectionErrors := []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: err.Error()}}
		return reconcile.Result{}, updateOrCreateConnectionPodStatus(ctx, r.reader, r.writer, r.scheme, connObj, connectionErrors, nil, r.getPod)
	}
	err = r.system.UpsertConnection(ctx, connObj.Spec.Config.Value, request.Name, connObj.Spec.Driver)
	if err != nil {
		log.Error(err, "failed to upsert connection", "name", request.Name)
		return reconcile.Result{Requeue: true}, updateOrCreateConnectionPodStatus(ctx, r.reader, r.writer, r.scheme, connObj, []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: err.Error()}}, nil, r.getPod)
	}

	log.Info("Connection upsert successful", "name", request.Name, "driver", connObj.Spec.Driver)
	return reconcile.Result{}, updateOrCreateConnectionPodStatus(ctx, r.reader, r.writer, r.scheme, connObj, nil, nil, r.getPod)
}

// UpdateOrCreateConnectionPodStatus records Connection reconciliation errors for
// the current pod without modifying source-owned publishing status.
func UpdateOrCreateConnectionPodStatus(
	ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	scheme *runtime.Scheme,
	connObjName string,
	connectionErrors []*statusv1alpha1.ConnectionError,
	getPod func(context.Context) (*corev1.Pod, error),
) error {
	request := types.NamespacedName{
		Namespace: util.GetNamespace(),
		Name:      connObjName,
	}
	connObj := &connectionv1alpha1.Connection{}
	err := reader.Get(ctx, request, connObj)
	if err != nil {
		return err
	}
	return updateOrCreateConnectionPodStatus(ctx, reader, writer, scheme, connObj, connectionErrors, nil, getPod)
}

// UpdateConnectionPodPublishStatus replaces one source's publishing status and
// retries conflicts so concurrent publishers merge against the latest object.
func UpdateConnectionPodPublishStatus(
	ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	scheme *runtime.Scheme,
	connObjName string,
	publishStatus statusv1alpha1.ConnectionPublishStatus,
	getPod func(context.Context) (*corev1.Pod, error),
) error {
	switch publishStatus.Source {
	case statusv1alpha1.AuditPublishSource, statusv1alpha1.WebhookPublishSource:
	default:
		return fmt.Errorf("unsupported publish status source %q", publishStatus.Source)
	}

	request := types.NamespacedName{
		Namespace: util.GetNamespace(),
		Name:      connObjName,
	}
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}, func() error {
		connObj := &connectionv1alpha1.Connection{}
		if err := reader.Get(ctx, request, connObj); err != nil {
			return err
		}
		return updateOrCreateConnectionPodStatus(ctx, reader, writer, scheme, connObj, nil, &publishStatus, getPod)
	})
}

func updateOrCreateConnectionPodStatus(ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	scheme *runtime.Scheme,
	connObj *connectionv1alpha1.Connection,
	connectionErrors []*statusv1alpha1.ConnectionError,
	publishStatus *statusv1alpha1.ConnectionPublishStatus,
	getPod func(context.Context) (*corev1.Pod, error),
) error {
	pod, err := getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}

	// Check if it exists already
	statusNS := pod.Namespace
	statusName, err := statusv1alpha1.KeyForConnection(pod.Name, connObj.GetNamespace(), connObj.GetName())
	if err != nil {
		return fmt.Errorf("getting key for connection: %w", err)
	}
	shouldCreate := true
	connPodStatusObj := &statusv1alpha1.ConnectionPodStatus{}
	var oldStatus *statusv1alpha1.ConnectionPodStatus

	err = reader.Get(ctx, types.NamespacedName{Namespace: statusNS, Name: statusName}, connPodStatusObj)

	switch {
	case err == nil:
		shouldCreate = false
		oldStatus = connPodStatusObj.DeepCopy()
	case apierrors.IsNotFound(err):
		if connPodStatusObj, err = newConnectionPodStatus(scheme, pod, connObj); err != nil {
			return fmt.Errorf("creating new connection connPodStatusObj: %w", err)
		}
	default:
		return fmt.Errorf("getting connection object status in name %s, namespace %s: %w", connObj.GetName(), connObj.GetNamespace(), err)
	}
	if !shouldCreate {
		if err := repairConnectionPodStatusMetadata(scheme, connPodStatusObj, pod, connObj); err != nil {
			return fmt.Errorf("repairing connection status metadata: %w", err)
		}
	}

	if !shouldCreate {
		if err := repairConnectionPodStatusMetadata(scheme, connPodStatusObj, pod, connObj); err != nil {
			return fmt.Errorf("repairing connection status metadata: %w", err)
		}
	}

	generationChanged := connPodStatusObj.Status.ObservedGeneration != connObj.GetGeneration()
	if generationChanged {
		connPodStatusObj.Status.PublishStatuses = nil
		connPodStatusObj.Status.ConnectionErrors = nil
	}

	if publishStatus != nil {
		setConnectionPublishStatus(&connPodStatusObj.Status, *publishStatus)
	} else {
		setConnectionErrors(&connPodStatusObj.Status, connectionErrors)
	}

	// ObservedGeneration is used to track the generation of the Connection object
	connPodStatusObj.Status.ObservedGeneration = connObj.GetGeneration()

	if shouldCreate {
		log.Info("Creating new ConnectionPodStatus object", "name", connPodStatusObj.GetName())
		return writer.Create(ctx, connPodStatusObj)
	}
	if apiequality.Semantic.DeepEqual(connPodStatusObj, oldStatus) {
		return nil
	}
	log.Info("Updating existing ConnectionPodStatus object", "name", connPodStatusObj.GetName())
	return writer.Update(ctx, connPodStatusObj)
}

func repairConnectionPodStatusMetadata(scheme *runtime.Scheme, status *statusv1alpha1.ConnectionPodStatus, pod *corev1.Pod, connObj *connectionv1alpha1.Connection) error {
	mergeLabels(status, map[string]string{
		statusv1beta1.ConnectionNameLabel: connObj.GetName(),
		statusv1beta1.PodLabel:            pod.Name,
	})
	if scheme == nil {
		return nil
	}
	return controllerutil.SetOwnerReference(pod, status, scheme)
}

func mergeLabels(obj client.Object, labels map[string]string) {
	merged := obj.GetLabels()
	if merged == nil {
		merged = make(map[string]string, len(labels))
	}
	for key, value := range labels {
		merged[key] = value
	}
	obj.SetLabels(merged)
}

// setConnectionPublishStatus updates one keyed source while retaining every
// other source. A nil last success carries forward the previous successful time.
func setConnectionPublishStatus(status *statusv1alpha1.ConnectionPodStatusStatus, replacement statusv1alpha1.ConnectionPublishStatus) {
	if len(replacement.Errors) > exportutil.MaxConnectionStatusErrors {
		replacement.Errors = replacement.Errors[:exportutil.MaxConnectionStatusErrors]
	}

	replaced := false
	for i := range status.PublishStatuses {
		if status.PublishStatuses[i].Source != replacement.Source {
			continue
		}
		if replacement.LastSuccessTime == nil {
			replacement.LastSuccessTime = status.PublishStatuses[i].LastSuccessTime
		}
		status.PublishStatuses[i] = replacement
		replaced = true
		break
	}
	if !replaced {
		status.PublishStatuses = append(status.PublishStatuses, replacement)
	}
	sort.Slice(status.PublishStatuses, func(i, j int) bool {
		return status.PublishStatuses[i].Source < status.PublishStatuses[j].Source
	})
}

func setConnectionErrors(status *statusv1alpha1.ConnectionPodStatusStatus, connectionErrors []*statusv1alpha1.ConnectionError) {
	if len(connectionErrors) > exportutil.MaxConnectionStatusErrors {
		connectionErrors = connectionErrors[:exportutil.MaxConnectionStatusErrors]
	}
	status.ConnectionErrors = connectionErrors
}

func deleteStatus(ctx context.Context,
	writer client.Writer,
	connectionNamespace string,
	connectionName string,
	getPod func(context.Context) (*corev1.Pod, error),
) error {
	connPodStatusObj := &statusv1alpha1.ConnectionPodStatus{}
	pod, err := getPod(ctx)
	if err != nil {
		return fmt.Errorf("getting reconciler pod: %w", err)
	}
	sName, err := statusv1alpha1.KeyForConnection(pod.Name, connectionNamespace, connectionName)
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
	connObj *connectionv1alpha1.Connection,
) (*statusv1alpha1.ConnectionPodStatus, error) {
	connPodStatusObj, err := statusv1alpha1.NewConnectionStatusForPod(pod, connObj.GetNamespace(), connObj.GetName(), scheme)
	if err != nil {
		return nil, fmt.Errorf("creating status for pod: %w", err)
	}
	connPodStatusObj.Status.ConnectionUID = connObj.GetUID()

	return connPodStatusObj, nil
}
