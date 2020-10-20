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

package assignmentmetadata

import (
	"context"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/api/errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	log = logf.Log.WithName("controller").WithValues(logging.Process, "assignmentmetadata_controller")
)

type Adder struct {
	Opa              *opa.Client
	WatchManager     *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	MutationCache    *mutation.Cache
}

// Add creates a new AssignMetadata Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {

	r := newReconciler(mgr, a.MutationCache)
	events := make(chan event.GenericEvent, 1024)
	return add(mgr, r, events)
}

func (a *Adder) InjectOpa(o *opa.Client) {
	a.Opa = o
}

func (a *Adder) InjectWatchManager(w *watch.Manager) {
	a.WatchManager = w
}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {
	a.ControllerSwitch = cs
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectMutationCache(c *mutation.Cache) {
	a.MutationCache = c
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, cache *mutation.Cache) *AssignMetadataReconciler {
	r := &AssignMetadataReconciler{cache: cache, Client: mgr.GetClient()}
	return r
}

type mapper struct {
	packer util.EventPacker
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, events <-chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New("assignmetadata-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided constraint
	err = c.Watch(
		&source.Channel{
			Source:         events,
			DestBufferSize: 1024,
		},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: util.EventPacker{}},
	)
	if err != nil {
		return err
	}

	// Watch for changes to ConstraintStatus
	err = c.Watch(
		&source.Kind{Type: &mutationsv1alpha1.AssignMetadata{}},
		&handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}

// AssignMetadataReconciler reconciles a AssignMetadata object
type AssignMetadataReconciler struct {
	client.Client
	cache *mutation.Cache
}

// +kubebuilder:rbac:groups=assignmetadata.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a constraint object and makes changes based on the state read
// and what is in the constraint.Spec
func (r *AssignMetadataReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconcile", "request", request)
	deleted := false
	m := &mutationsv1alpha1.AssignMetadata{}
	err := r.Get(context.TODO(), request.NamespacedName, m)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		deleted = true
		// be sure we are using a blank constraint template so that
		// we know finalizer removal code won't break (can be removed once that
		// code is removed)
		m = &mutationsv1alpha1.AssignMetadata{}
	}
	deleted = deleted || !m.GetDeletionTimestamp().IsZero()

	if !deleted {
		err := r.cache.Insert(mutation.MetadataMutator{AssignMetadata: m.DeepCopy()})
		if err != nil {
			log.Error(err, "Failed to insert")
		}
	} else {
		err := r.cache.Remove(mutation.MetadataMutator{AssignMetadata: m.DeepCopy()})
		if err != nil {
			log.Error(err, "Failed to remove")
		}
	}
	return ctrl.Result{}, nil
}
