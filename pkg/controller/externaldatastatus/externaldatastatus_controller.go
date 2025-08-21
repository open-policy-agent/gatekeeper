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

package externaldatastatus

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller").WithValues(logging.Process, "externaldata_status_controller")

type Adder struct {
	WatchManager *watch.Manager
}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

// Add creates a new provider Status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.IsAssigned(operations.Status) {
		return nil
	}
	r := newReconciler(mgr)
	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) *ReconcileProviderStatus {
	return &ReconcileProviderStatus{
		// Separate reader and writer because manager's default client bypasses the cache for unstructured resources.
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		reader:       mgr.GetCache(),
		scheme:       mgr.GetScheme(),
		log:          log,
	}
}

// PodStatusToProviderMapper correlates a ProviderPodStatus with its corresponding Provider.
// `selfOnly` tells the mapper to only map statuses corresponding to the current pod.
func PodStatusToProviderMapper(selfOnly bool) handler.TypedMapFunc[*statusv1beta1.ProviderPodStatus, reconcile.Request] {
	return func(_ context.Context, obj *statusv1beta1.ProviderPodStatus) []reconcile.Request {
		labels := obj.GetLabels()
		name, ok := labels[statusv1beta1.ProviderNameLabel]
		if !ok {
			log.Error(fmt.Errorf("provider status reskource with no mapping label: %s", obj.GetName()), "missing label while attempting to map a provider status resource")
			return nil
		}
		if selfOnly {
			pod, ok := labels[statusv1beta1.PodLabel]
			if !ok {
				log.Error(fmt.Errorf("provider status resource with no pod label: %s", obj.GetName()), "missing label while attempting to map a provider status resource")
			}
			// Do not attempt to reconcile the resource when other pods have changed their status
			if pod != util.GetPodName() {
				return nil
			}
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name}}}
	}
}

// Add creates a new externaldata status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("externaldata-status-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(
			mgr.GetCache(), &statusv1beta1.ProviderPodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(PodStatusToProviderMapper(false))),
	)
	if err != nil {
		return err
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &externaldatav1beta1.Provider{},
			&handler.TypedEnqueueRequestForObject[*externaldatav1beta1.Provider]{}))
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileProviderStatus{}

// ReconcileProviderStatus provides the dependencies required to reconcile the status of a Provider resource.
type ReconcileProviderStatus struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme *runtime.Scheme
	log    logr.Logger
}

// +kubebuilder:rbac:groups=externaldata.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads the state of the cluster for a Provider object and makes changes based on the ProviderPodStatuses.
func (r *ReconcileProviderStatus) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile request", "namespace", request.Namespace, "name", request.Name)

	providerObj := &externaldatav1beta1.Provider{}
	err := r.reader.Get(ctx, request.NamespacedName, providerObj)
	if err != nil {
		// If the Provider does not exist then we are done
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	sObjs := &statusv1beta1.ProviderPodStatusList{}
	if err := r.reader.List(
		ctx,
		sObjs,
		client.MatchingLabels{statusv1beta1.ProviderNameLabel: request.Name},
		client.InNamespace(util.GetNamespace()),
	); err != nil {
		return reconcile.Result{}, err
	}

	statusObjs := make(sortableStatuses, len(sObjs.Items))
	copy(statusObjs, sObjs.Items)
	sort.Sort(statusObjs)

	var s []externaldatav1beta1.ProviderPodStatusStatus

	for i := range statusObjs {
		// Don't report status if it's not for the correct object. This can happen
		// if a watch gets interrupted, causing the status to be deleted out from underneath it
		if statusObjs[i].Status.ProviderUID != providerObj.GetUID() {
			continue
		}
		s = append(s, toProviderPodStatusStatus(&statusObjs[i].Status))
	}

	providerObj.Status.ByPod = s

	// Update the status of the Provider resource
	if err := r.statusClient.Status().Update(ctx, providerObj); err != nil {
		log.Error(err, "failed to update provider status")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

type sortableStatuses []statusv1beta1.ProviderPodStatus

func (s sortableStatuses) Len() int {
	return len(s)
}

func (s sortableStatuses) Less(i, j int) bool {
	return s[i].Status.ID < s[j].Status.ID
}

func (s sortableStatuses) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func toProviderPodStatusStatus(status *statusv1beta1.ProviderPodStatusStatus) externaldatav1beta1.ProviderPodStatusStatus {
	errs := make([]externaldatav1beta1.ProviderError, len(status.Errors))
	for i, err := range status.Errors {
		errs[i] = externaldatav1beta1.ProviderError{
			Type:           externaldatav1beta1.ProviderErrorType(err.Type),
			Message:        err.Message,
			Retryable:      err.Retryable,
			ErrorTimestamp: err.ErrorTimestamp,
		}
	}
	convertedStatus := externaldatav1beta1.ProviderPodStatusStatus{
		ID:                  status.ID,
		ProviderUID:         status.ProviderUID,
		Active:              status.Active,
		Errors:              errs,
		Operations:          status.Operations,
		LastTransitionTime:  status.LastTransitionTime,
		LastCacheUpdateTime: status.LastCacheUpdateTime,
		ObservedGeneration:  status.ObservedGeneration,
	}
	return convertedStatus
}
