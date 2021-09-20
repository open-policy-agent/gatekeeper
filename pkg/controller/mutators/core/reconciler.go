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

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	ctrlmutators "github.com/open-policy-agent/gatekeeper/pkg/controller/mutators"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	mutationschema "github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(
	mgr manager.Manager,
	mutationSystem *mutation.System,
	tracker *readiness.Tracker,
	getPod func(context.Context) (*corev1.Pod, error),
	kind string,
	newMutationObj func() client.Object,
	mutatorFor func(client.Object) (types.Mutator, error),
) *Reconciler {
	r := &Reconciler{
		system:           mutationSystem,
		Client:           mgr.GetClient(),
		tracker:          tracker,
		getPod:           getPod,
		scheme:           mgr.GetScheme(),
		reporter:         ctrlmutators.NewStatsReporter(),
		cache:            ctrlmutators.NewMutationCache(),
		kind:             kind,
		newMutationObj:   newMutationObj,
		mutatorFor:       mutatorFor,
		log:              logf.Log.WithName("controller").WithValues(logging.Process, fmt.Sprintf("%s_controller", strings.ToLower(kind))),
		podOwnershipMode: statusv1beta1.GetPodOwnershipMode(),
	}
	if getPod == nil {
		r.getPod = r.defaultGetPod
	}
	return r
}

// Reconciler reconciles mutator objects.
type Reconciler struct {
	client.Client
	kind           string
	newMutationObj func() client.Object
	mutatorFor     func(client.Object) (types.Mutator, error)

	system   *mutation.System
	tracker  *readiness.Tracker
	getPod   func(context.Context) (*corev1.Pod, error)
	scheme   *runtime.Scheme
	reporter ctrlmutators.StatsReporter
	cache    *ctrlmutators.Cache
	log      logr.Logger

	podOwnershipMode statusv1beta1.PodOwnershipMode
}

// +kubebuilder:rbac:groups=mutations.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a mutator object and syncs it with the mutation system.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	r.log.Info("Reconcile", "request", request)
	startTime := time.Now()

	mutationObj, deleted, err := r.getOrDefault(ctx, request.NamespacedName)
	if err != nil {
		return reconcile.Result{}, err
	}

	ingestionStatus := ctrlmutators.MutatorStatusError
	// Encasing this call in a function prevents the arguments from being evaluated early.
	id := types.MakeID(mutationObj)
	defer func() {
		r.reportMutator(id, ingestionStatus, startTime)
	}()

	conflicts := r.system.GetConflicts(id)

	if deleted {
		// Either the mutator was deleted before we were able to process this request, or it has been marked for
		// deletion.
		r.getTracker().CancelExpect(mutationObj)
		err = r.reconcileDeleted(ctx, id)
	} else {
		err = r.reconcileUpsert(ctx, id, mutationObj)
	}

	if err != nil {
		return reconcile.Result{}, err
	}

	r.updateConflicts(ctx, id, conflicts)

	ingestionStatus = ctrlmutators.MutatorStatusActive
	return reconcile.Result{}, nil
}

func (r *Reconciler) reconcileUpsert(ctx context.Context, id types.ID, mutationObj client.Object) error {
	mutator, err := r.mutatorFor(mutationObj)
	if err != nil {
		r.log.Error(err, "Creating mutator for resource failed", "resource",
			client.ObjectKey{Namespace: mutationObj.GetNamespace(), Name: mutationObj.GetName()})
		r.getTracker().TryCancelExpect(mutationObj)

		return r.updateError(ctx, mutationObj, err)
	}

	if err = r.system.Upsert(mutator); err != nil {
		r.log.Error(err, "Insert failed", "resource",
			client.ObjectKey{Namespace: mutationObj.GetNamespace(), Name: mutationObj.GetName()})
		r.getTracker().TryCancelExpect(mutationObj)

		return r.upsertError(ctx, mutationObj, err)
	}

	r.getTracker().Observe(mutationObj)

	return r.updateStatus(ctx, id,
		setID(mutationObj.GetUID()), setGeneration(mutationObj.GetGeneration()),
		setEnforced(true), setErrors(nil))
}

func (r *Reconciler) getOrCreatePodStatus(ctx context.Context, mutatorID types.ID) (*statusv1beta1.MutatorPodStatus, error) {
	pod, err := r.getPod(ctx)
	if err != nil {
		return nil, err
	}

	statusObj := &statusv1beta1.MutatorPodStatus{}
	sName, err := statusv1beta1.KeyForMutatorID(pod.Name, mutatorID)
	if err != nil {
		return nil, err
	}

	key := apiTypes.NamespacedName{Name: sName, Namespace: pod.Namespace}
	if err := r.Get(ctx, key, statusObj); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
	} else {
		return statusObj, nil
	}

	statusObj, err = statusv1beta1.NewMutatorStatusForPod(pod, r.podOwnershipMode, mutatorID, r.scheme)
	if err != nil {
		return nil, err
	}
	if err := r.Create(ctx, statusObj); err != nil {
		return nil, err
	}
	return statusObj, nil
}

func (r *Reconciler) defaultGetPod(_ context.Context) (*corev1.Pod, error) {
	// require injection of GetPod in order to control what client we use to
	// guarantee we don't inadvertently create a watch
	panic("GetPod must be injected to Reconciler")
}

func (r *Reconciler) reportMutator(mID types.ID, ingestionStatus ctrlmutators.MutatorIngestionStatus, startTime time.Time) {
	r.cache.Upsert(mID, ingestionStatus)
	if r.reporter == nil {
		return
	}

	if err := r.reporter.ReportMutatorIngestionRequest(ingestionStatus, time.Since(startTime)); err != nil {
		r.log.Error(err, "failed to report mutator ingestion request")
	}

	for status, count := range r.cache.Tally() {
		if err := r.reporter.ReportMutatorsStatus(status, count); err != nil {
			r.log.Error(err, "failed to report mutator status request")
		}
	}
}

func (r *Reconciler) gvk() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   mutationsv1alpha1.GroupVersion.Group,
		Version: mutationsv1alpha1.GroupVersion.Version,
		Kind:    r.kind,
	}
}

// getOrDefault attempts to get the Mutator from the cluster, or returns a default-instantiated Mutator if one does not
// exist.
func (r *Reconciler) getOrDefault(ctx context.Context, namespacedName apiTypes.NamespacedName) (client.Object, bool, error) {
	obj := r.newMutationObj()
	err := r.Get(ctx, namespacedName, obj)
	switch {
	case err == nil:
		// Treat objects with a DeletionTimestamp as if they are deleted.
		deleted := !obj.GetDeletionTimestamp().IsZero()
		return obj, deleted, nil
	case apierrors.IsNotFound(err):
		obj = r.newMutationObj()
		obj.SetName(namespacedName.Name)
		obj.SetNamespace(namespacedName.Namespace)
		obj.GetObjectKind().SetGroupVersionKind(r.gvk())
		return obj, true, nil
	default:
		return nil, false, err
	}
}

func (r *Reconciler) getTracker() readiness.Expectations {
	return r.tracker.For(r.gvk())
}

// reconcileDeleted removes the Mutator from the controller and deletes the corresponding PodStatus.
func (r *Reconciler) reconcileDeleted(ctx context.Context, mID types.ID) error {
	r.cache.Remove(mID)

	if err := r.system.Remove(mID); err != nil {
		r.log.Error(err, "Remove failed", "resource",
			apiTypes.NamespacedName{Name: mID.Name, Namespace: mID.Namespace})
		return err
	}

	pod, err := r.getPod(ctx)
	if err != nil {
		return err
	}

	sName, err := statusv1beta1.KeyForMutatorID(pod.Name, mID)
	if err != nil {
		return err
	}

	status := &statusv1beta1.MutatorPodStatus{}
	status.SetName(sName)
	status.SetNamespace(pod.Namespace)
	if err = r.Delete(ctx, status); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (r *Reconciler) updateConflicts(ctx context.Context, id types.ID, conflicts []types.ID) {
	for _, conflict := range conflicts {
		idConflicts := r.system.GetConflicts(conflict)
		if conflict == id {
			continue
		}
		if len(idConflicts) == 0 {
			_ = r.updateStatus(ctx, conflict, updateConflictStatus(nil))
		} else {
			_ = r.updateStatus(ctx, conflict, updateConflictStatus(mutationschema.NewErrConflictingSchema(idConflicts)))
		}
	}
}

// updateStatus updates the PodStatus corresponding to the passed Mutator with whether the Mutator is enforced, and
// whether there is an error instantiating the Mutator within the controller.
func (r *Reconciler) updateStatus(ctx context.Context, id types.ID, updates ...statusUpdate) error {
	status, err := r.getOrCreatePodStatus(ctx, id)
	if err != nil {
		r.log.Info("could not get/create pod status object", "error", err)
		return err
	}

	for _, update := range updates {
		update(status)
	}

	if err = r.Update(ctx, status); err != nil {
		r.log.Error(err, "could not update mutator status")
	}

	return nil
}

func (r *Reconciler) updateError(ctx context.Context, obj client.Object, err error) error {
	id := types.MakeID(obj)

	return r.updateStatus(ctx, id,
		setID(obj.GetUID()), setGeneration(obj.GetGeneration()),
		setEnforced(false), setErrors(err))
}

func (r *Reconciler) upsertError(ctx context.Context, obj client.Object, errToUpsert error) error {
	schemaErr := &mutationschema.ErrConflictingSchema{}

	err := r.updateError(ctx, obj, errToUpsert)
	if !errors.As(errToUpsert, schemaErr) {
		return err
	}

	objID := types.MakeID(obj)

	ids := schemaErr.Conflicts
	for _, id := range ids {
		if id == objID {
			continue
		}
		_ = r.updateStatus(ctx, id, setEnforced(false), setErrors(errToUpsert))
	}

	return err
}
