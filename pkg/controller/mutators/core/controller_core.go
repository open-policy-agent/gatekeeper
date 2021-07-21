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
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	ctrlmutators "github.com/open-policy-agent/gatekeeper/pkg/controller/mutators"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutatorstatus"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type Adder struct {
	// MutationSystem holds a reference to the mutation system to which
	// mutators will be registered/deregistered
	MutationSystem *mutation.System
	// Tracker accepts a handle for the readiness tracker
	Tracker *readiness.Tracker
	// GetPod returns an instance of the currently running Gatekeeper pod
	GetPod func(ctx context.Context) (*corev1.Pod, error)
	// Kind for the mutation object that is being reconciled
	Kind string
	// NewMutationObj creates a new instance of a mutation struct that can
	// be fed to the API server client for Get/Delete/Update requests
	NewMutationObj func() client.Object
	// MutatorFor takes the object returned by NewMutationObject and
	// turns it into a mutator. The contents of the mutation object
	// are set by the API server.
	MutatorFor func(client.Object) (types.Mutator, error)
}

// Add creates a new Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	r := newReconciler(mgr, a.MutationSystem, a.Tracker, a.GetPod, a.Kind, a.NewMutationObj, a.MutatorFor)
	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(
	mgr manager.Manager,
	mutationSystem *mutation.System,
	tracker *readiness.Tracker,
	getPod func(ctx context.Context) (*corev1.Pod, error),
	kind string,
	newMutationObj func() client.Object,
	mutatorFor func(client.Object) (types.Mutator, error),
) *Reconciler {
	r := &Reconciler{
		system:         mutationSystem,
		Client:         mgr.GetClient(),
		tracker:        tracker,
		getPod:         getPod,
		scheme:         mgr.GetScheme(),
		reporter:       ctrlmutators.NewStatsReporter(),
		cache:          ctrlmutators.NewMutationCache(),
		kind:           kind,
		newMutationObj: newMutationObj,
		mutatorFor:     mutatorFor,
		log:            logf.Log.WithName("controller").WithValues(logging.Process, fmt.Sprintf("%s_controller", strings.ToLower(kind))),
	}
	if getPod == nil {
		r.getPod = r.defaultGetPod
	}
	return r
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r *Reconciler) error {
	if !*mutation.MutationEnabled {
		return nil
	}

	// Create a new controller
	c, err := controller.New(fmt.Sprintf("%s-controller", strings.ToLower(r.kind)), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the mutator
	if err = c.Watch(
		&source.Kind{Type: r.newMutationObj()},
		&handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	// Watch for changes to MutatorPodStatus
	err = c.Watch(
		&source.Kind{Type: &statusv1beta1.MutatorPodStatus{}},
		handler.EnqueueRequestsFromMapFunc(mutatorstatus.PodStatusToMutatorMapper(true, r.kind, util.EventPackerMapFunc())),
	)
	if err != nil {
		return err
	}
	return nil
}

// Reconciler reconciles mutator objects.
type Reconciler struct {
	client.Client
	kind           string
	newMutationObj func() client.Object
	mutatorFor     func(client.Object) (types.Mutator, error)

	system   *mutation.System
	tracker  *readiness.Tracker
	getPod   func(ctx context.Context) (*corev1.Pod, error)
	scheme   *runtime.Scheme
	reporter ctrlmutators.StatsReporter
	cache    *ctrlmutators.Cache
	log      logr.Logger
}

// +kubebuilder:rbac:groups=mutations.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a mutator object and syncs it with the mutation system.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	r.log.Info("Reconcile", "request", request)
	startTime := time.Now()

	gvk := schema.GroupVersionKind{
		Group:   mutationsv1alpha1.GroupVersion.Group,
		Version: mutationsv1alpha1.GroupVersion.Version,
		Kind:    r.kind,
	}

	deleted := false
	mutationObj := r.newMutationObj()
	err := r.Get(ctx, request.NamespacedName, mutationObj)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		deleted = true
		mutationObj = r.newMutationObj()
		mutationObj.SetName(request.NamespacedName.Name)
		mutationObj.SetNamespace(request.NamespacedName.Namespace)
		mutationObj.GetObjectKind().SetGroupVersionKind(gvk)
	}
	deleted = deleted || !mutationObj.GetDeletionTimestamp().IsZero()
	tracker := r.tracker.For(gvk)

	mID := types.MakeID(mutationObj)

	if deleted {
		tracker.CancelExpect(mutationObj)
		r.cache.Remove(mID)

		if err := r.system.Remove(mID); err != nil {
			r.log.Error(err, "Remove failed", "resource", request.NamespacedName)
			return reconcile.Result{}, err
		}

		sName, err := statusv1beta1.KeyForMutatorID(util.GetPodName(), mID)
		if err != nil {
			return reconcile.Result{}, err
		}
		status := &statusv1beta1.MutatorPodStatus{}
		status.SetName(sName)
		status.SetNamespace(util.GetNamespace())
		if err := r.Delete(ctx, status); err != nil {
			if !errors.IsNotFound(err) {
				return reconcile.Result{}, err
			}
		}

		return reconcile.Result{}, nil
	}

	ingestionStatus := ctrlmutators.MutatorStatusError
	// encasing this call in a function prevents the arguments from being evaluated early
	defer func() { r.reportMutator(mID, ingestionStatus, startTime) }()

	status, err := r.getOrCreatePodStatus(ctx, mID)
	if err != nil {
		r.log.Info("could not get/create pod status object", "error", err)
		return reconcile.Result{}, err
	}
	status.Status.MutatorUID = mutationObj.GetUID()
	status.Status.ObservedGeneration = mutationObj.GetGeneration()
	status.Status.Errors = nil

	mutator, err := r.mutatorFor(mutationObj)
	if err != nil {
		r.log.Error(err, "Creating mutator for resource failed", "resource", request.NamespacedName)
		tracker.TryCancelExpect(mutationObj)
		status.Status.Errors = append(status.Status.Errors, statusv1beta1.MutatorError{Message: err.Error()})
		if err2 := r.Update(ctx, status); err != nil {
			r.log.Error(err2, "could not update mutator status")
		}
		return reconcile.Result{}, err
	}

	if err := r.system.Upsert(mutator); err != nil {
		r.log.Error(err, "Insert failed", "resource", request.NamespacedName)
		tracker.TryCancelExpect(mutationObj)
		status.Status.Errors = append(status.Status.Errors, statusv1beta1.MutatorError{Message: err.Error()})
		if err2 := r.Update(ctx, status); err != nil {
			r.log.Error(err2, "could not update mutator status")
		}
		return reconcile.Result{}, err
	}

	tracker.Observe(mutationObj)
	status.Status.Enforced = true

	if err := r.Update(ctx, status); err != nil {
		r.log.Error(err, "could not update mutator status")
		return reconcile.Result{}, err
	}

	ingestionStatus = ctrlmutators.MutatorStatusActive
	return reconcile.Result{}, nil
}

func (r *Reconciler) getOrCreatePodStatus(ctx context.Context, mutatorID types.ID) (*statusv1beta1.MutatorPodStatus, error) {
	statusObj := &statusv1beta1.MutatorPodStatus{}
	sName, err := statusv1beta1.KeyForMutatorID(util.GetPodName(), mutatorID)
	if err != nil {
		return nil, err
	}
	key := apiTypes.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
	if err := r.Get(context.TODO(), key, statusObj); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
	} else {
		return statusObj, nil
	}
	pod, err := r.getPod(ctx)
	if err != nil {
		return nil, err
	}
	statusObj, err = statusv1beta1.NewMutatorStatusForPod(pod, mutatorID, r.scheme)
	if err != nil {
		return nil, err
	}
	if err := r.Create(context.TODO(), statusObj); err != nil {
		return nil, err
	}
	return statusObj, nil
}

func (r *Reconciler) defaultGetPod(ctx context.Context) (*corev1.Pod, error) {
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
