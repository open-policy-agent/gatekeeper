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

package connectionstatus

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"
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

var log = logf.Log.WithName("controller").WithValues(logging.Process, "connection_status_controller")

type Adder struct {
	WatchManager *watch.Manager
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

// Add creates a new connection Status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.IsAssigned(operations.Status) {
		return nil
	}
	r := newReconciler(mgr)
	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) *ReconcileConnectionStatus {
	return &ReconcileConnectionStatus{
		// Separate reader and writer because manager's default client bypasses the cache for unstructured resources.
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		reader:       mgr.GetCache(),
		scheme:       mgr.GetScheme(),
		log:          log,
	}
}

// PodStatusToConnectionMapper correlates a ConnectionPodStatus with its corresponding Connection.
// `selfOnly` tells the mapper to only map statuses corresponding to the current pod.
func PodStatusToConnectionMapper(selfOnly bool) handler.TypedMapFunc[*statusv1alpha1.ConnectionPodStatus, reconcile.Request] {
	return func(_ context.Context, obj *statusv1alpha1.ConnectionPodStatus) []reconcile.Request {
		labels := obj.GetLabels()
		connObjName, ok := labels[statusv1beta1.ConnectionNameLabel]
		if !ok {
			log.Error(fmt.Errorf("connection status resource with no mapping label: %s", obj.GetName()), "missing label while attempting to map a connection status resource")
			return nil
		}
		if selfOnly {
			pod, ok := labels[statusv1beta1.PodLabel]
			if !ok {
				log.Error(fmt.Errorf("connection status resource with no pod label: %s", obj.GetName()), "missing label while attempting to map a connection status resource")
			}
			// Do not attempt to reconcile the resource when other pods have changed their status
			if pod != util.GetPodName() {
				return nil
			}
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Name:      connObjName,
			Namespace: obj.Namespace,
		}}}
	}
}

// Add creates a new connection status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("connection-status-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &statusv1alpha1.ConnectionPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(PodStatusToConnectionMapper(false)),
			predicate.TypedFuncs[*statusv1alpha1.ConnectionPodStatus]{
				CreateFunc: func(e event.TypedCreateEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*statusv1alpha1.ConnectionPodStatus]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace()
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

	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &v1alpha1.Connection{},
			&handler.TypedEnqueueRequestForObject[*v1alpha1.Connection]{},
			predicate.TypedFuncs[*v1alpha1.Connection]{
				CreateFunc: func(e event.TypedCreateEvent[*v1alpha1.Connection]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				UpdateFunc: func(e event.TypedUpdateEvent[*v1alpha1.Connection]) bool {
					return e.ObjectNew.GetNamespace() == util.GetNamespace()
				},
				DeleteFunc: func(e event.TypedDeleteEvent[*v1alpha1.Connection]) bool {
					return e.Object.GetNamespace() == util.GetNamespace()
				},
				GenericFunc: func(e event.TypedGenericEvent[*v1alpha1.Connection]) bool {
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

var _ reconcile.Reconciler = &ReconcileConnectionStatus{}

// ReconcileConnectionStatus provides the dependencies required to reconcile the status of a Connection resource.
type ReconcileConnectionStatus struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme *runtime.Scheme
	log    logr.Logger
}

// +kubebuilder:rbac:groups=connection.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads the state of the cluster for a Connection object and makes changes based on the ConnectionPodStatuses.
func (r *ReconcileConnectionStatus) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile request", "namespace", request.Namespace, "name", request.Name)

	connObj := &v1alpha1.Connection{}
	err := r.reader.Get(ctx, request.NamespacedName, connObj)
	if err != nil {
		// If the Connection does not exist then we are done
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	sObjs := &statusv1alpha1.ConnectionPodStatusList{}
	if err := r.reader.List(
		ctx,
		sObjs,
		client.MatchingLabels{statusv1beta1.ConnectionNameLabel: request.Name},
		client.InNamespace(util.GetNamespace()),
	); err != nil {
		return reconcile.Result{}, err
	}
	statusObjs := make(sortableStatuses, len(sObjs.Items))
	copy(statusObjs, sObjs.Items)
	sort.Sort(statusObjs)

	var s []statusv1alpha1.ConnectionPodStatusStatus

	for i := range statusObjs {
		// Don't report status if it's not for the correct object. This can happen
		// if a watch gets interrupted, causing the status to be deleted out from underneath it
		if statusObjs[i].Status.ConnectionUID != connObj.GetUID() {
			continue
		}
		s = append(s, statusObjs[i].Status)
	}

	connObj.Status.ByPod = s

	// Update the status of the Connection resource
	if err := r.statusClient.Status().Update(ctx, connObj); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

type sortableStatuses []statusv1alpha1.ConnectionPodStatus

func (s sortableStatuses) Len() int {
	return len(s)
}

func (s sortableStatuses) Less(i, j int) bool {
	return s[i].Status.ID < s[j].Status.ID
}

func (s sortableStatuses) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
