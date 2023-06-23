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

package sync

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

var log = logf.Log.WithName("controller").WithValues("metaKind", "Sync")

type Adder struct {
	CacheManager *cm.CacheManager
	Events       <-chan event.GenericEvent
}

// Add creates a new Sync Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !operations.HasValidationOperations() {
		return nil
	}
	reporter, err := syncutil.NewStatsReporter()
	if err != nil {
		log.Error(err, "Sync metrics reporter could not start")
		return err
	}

	r := newReconciler(mgr, *reporter, a.CacheManager)
	return add(mgr, r, a.Events)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(
	mgr manager.Manager,
	reporter syncutil.Reporter,
	cmt *cm.CacheManager,
) reconcile.Reconciler {
	return &ReconcileSync{
		reader:   mgr.GetCache(),
		scheme:   mgr.GetScheme(),
		log:      log,
		reporter: reporter,
		cm:       cmt,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler, events <-chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New("sync-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided resource
	return c.Watch(
		&source.Channel{
			Source:         events,
			DestBufferSize: 1024,
		},
		handler.EnqueueRequestsFromMapFunc(util.EventPackerMapFunc()),
	)
}

var _ reconcile.Reconciler = &ReconcileSync{}

// ReconcileSync reconciles an arbitrary object described by Kind.
type ReconcileSync struct {
	reader client.Reader

	scheme   *runtime.Scheme
	log      logr.Logger
	reporter syncutil.Reporter
	cm       *cm.CacheManager
}

// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for an object and makes changes based on the state read
// and what is in the constraint.Spec.
func (r *ReconcileSync) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	timeStart := time.Now()

	gvk, unpackedRequest, err := util.UnpackRequest(request)
	if err != nil {
		// Unrecoverable, do not retry.
		// TODO(OREN) add metric
		log.Error(err, "unpacking request", "request", request)
		return reconcile.Result{}, nil
	}

	reportMetrics := false
	defer func() {
		if reportMetrics {
			if err := r.reporter.ReportSyncDuration(time.Since(timeStart)); err != nil {
				log.Error(err, "failed to report sync duration")
			}

			r.cm.ReportSyncMetrics()

			if err := r.reporter.ReportLastSync(); err != nil {
				log.Error(err, "failed to report last sync timestamp")
			}
		}
	}()

	instance := &unstructured.Unstructured{}
	instance.SetGroupVersionKind(gvk)

	if err := r.reader.Get(ctx, unpackedRequest.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// This is a deletion; remove the data
			instance.SetNamespace(unpackedRequest.Namespace)
			instance.SetName(unpackedRequest.Name)
			if err := r.cm.RemoveObject(ctx, instance); err != nil {
				return reconcile.Result{}, err
			}

			reportMetrics = true
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if !instance.GetDeletionTimestamp().IsZero() {
		if err := r.cm.RemoveObject(ctx, instance); err != nil {
			return reconcile.Result{}, err
		}

		reportMetrics = true
		return reconcile.Result{}, nil
	}

	r.log.V(logging.DebugLevel).Info(
		"data will be added",
		logging.ResourceAPIVersion, instance.GetAPIVersion(),
		logging.ResourceKind, instance.GetKind(),
		logging.ResourceNamespace, instance.GetNamespace(),
		logging.ResourceName, instance.GetName(),
	)

	reportMetrics = true
	if err := r.cm.AddObject(ctx, instance); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
