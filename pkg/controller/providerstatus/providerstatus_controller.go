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

package providerstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	externaldatav1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/externaldata/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues(logging.Process, "provider_status_controller")

type Adder struct {
	Events <-chan event.GenericEvent
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

// Add creates a new Provider Status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.IsAssigned(operations.Status) {
		return nil
	}
	r := newReconciler(mgr)
	return add(mgr, r, a.Events)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(
	mgr manager.Manager,
) *ReconcileProviderStatus {
	return &ReconcileProviderStatus{
		// Separate reader and writer because manager's default client bypasses the cache for unstructured resources.
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		reader:       mgr.GetCache(),
		scheme:       mgr.GetScheme(),
		log:          log,
	}
}

type PackerMap func(obj client.Object) []reconcile.Request

// PodStatusToProviderMapper correlates a ProviderPodStatus with its corresponding provider
// `selfOnly` tells the mapper to only map statuses corresponding to the current pod.
func PodStatusToProviderMapper(selfOnly bool, packerMap handler.MapFunc) handler.TypedMapFunc[*v1beta1.ProviderPodStatus, reconcile.Request] {
	return func(ctx context.Context, obj *v1beta1.ProviderPodStatus) []reconcile.Request {
		labels := obj.GetLabels()
		name, ok := labels[v1beta1.ProviderNameLabel]
		if !ok {
			log.Error(fmt.Errorf("provider status resource with no name label: %s", obj.GetName()), "missing label while attempting to map a provider status resource")
			return nil
		}
		if selfOnly {
			myID, ok := labels[v1beta1.PodLabel]
			if !ok {
				log.Error(fmt.Errorf("provider status resource with no pod label: %s", obj.GetName()), "missing label while attempting to map a provider status resource")
				return nil
			}
			if myID != util.GetPodName() {
				return nil
			}
		}

		return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler, events <-chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New("provider-status-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to Provider
	err = c.Watch(
		source.Kind(mgr.GetCache(), &externaldatav1beta1.Provider{},
			&handler.TypedEnqueueRequestForObject[*externaldatav1beta1.Provider]{}))
	if err != nil {
		return err
	}

	// Watch for changes to ProviderPodStatus
	err = c.Watch(
		source.Kind(mgr.GetCache(), &v1beta1.ProviderPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(PodStatusToProviderMapper(false, nil))))
	if err != nil {
		return err
	}

	// Inject events if available
	if events != nil {
		err = c.Watch(
			source.Channel(
				events,
				&handler.EnqueueRequestForObject{},
			))
		if err != nil {
			return err
		}
	}

	return nil
}

// ReconcileProviderStatus reconciles a Provider object
type ReconcileProviderStatus struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient
	scheme       *runtime.Scheme
	log          logr.Logger
}

// +kubebuilder:rbac:groups=externaldata.gatekeeper.sh,resources=providers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=providerpodstatuses,verbs=get;list;watch

// Reconcile reads that state of the cluster for a Provider object and makes changes based on the state read
// and what is in the Provider.Spec
func (r *ReconcileProviderStatus) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the Provider instance
	instance := &externaldatav1beta1.Provider{}
	err := r.reader.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	sObjs := &v1beta1.ProviderPodStatusList{}
	if err := r.reader.List(ctx, sObjs,
		client.MatchingLabels{
			v1beta1.ProviderNameLabel: instance.GetName(),
		},
		client.InNamespace(util.GetNamespace()),
	); err != nil {
		return reconcile.Result{}, err
	}
	statusObjs := make(sortableStatuses, len(sObjs.Items))
	copy(statusObjs, sObjs.Items)
	sort.Sort(statusObjs)

	var s []interface{}
	for i := range statusObjs {
		// Don't report status if it's not for the correct object. This can happen
		// if a watch gets interrupted, causing the provider status to be deleted
		// out from underneath it
		if statusObjs[i].Status.ProviderUID != instance.GetUID() {
			continue
		}
		j, err := json.Marshal(statusObjs[i].Status)
		if err != nil {
			return reconcile.Result{}, err
		}
		var o map[string]interface{}
		if err := json.Unmarshal(j, &o); err != nil {
			return reconcile.Result{}, err
		}
		s = append(s, o)
	}

	// Update status
	instance.Status.ByPod = make([]v1beta1.ProviderPodStatusStatus, len(s))
	for i, status := range s {
		statusBytes, err := json.Marshal(status)
		if err != nil {
			return reconcile.Result{}, err
		}
		if err := json.Unmarshal(statusBytes, &instance.Status.ByPod[i]); err != nil {
			return reconcile.Result{}, err
		}
	}

	if err = r.statusClient.Status().Update(ctx, instance); err != nil {
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

type sortableStatuses []v1beta1.ProviderPodStatus

func (s sortableStatuses) Len() int {
	return len(s)
}

func (s sortableStatuses) Less(i, j int) bool {
	return s[i].Status.ID < s[j].Status.ID
}

func (s sortableStatuses) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}