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

package expansionstatus

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	expansionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
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

var log = logf.Log.WithName("controller").WithValues(logging.Process, "expansion_template_status_controller")

type Adder struct {
	CFClient     *constraintclient.Client
	WatchManager *watch.Manager
}

func (a *Adder) InjectControllerSwitch(_ *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(_ *readiness.Tracker) {}

// Add creates a new Constraint Status Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	if !*expansion.ExpansionEnabled {
		return nil
	}
	r := newReconciler(mgr)
	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileExpansionStatus{
		// Separate reader and writer because manager's default client bypasses the cache for unstructured resources.
		writer:       mgr.GetClient(),
		statusClient: mgr.GetClient(),
		reader:       mgr.GetCache(),

		scheme: mgr.GetScheme(),
		log:    log,
	}
}

// PodStatusToExpansionTemplateMapper correlates a ExpansionTemplatePodStatus with its corresponding expansion template.
// `selfOnly` tells the mapper to only map statuses corresponding to the current pod.
func PodStatusToExpansionTemplateMapper(selfOnly bool) handler.TypedMapFunc[*v1beta1.ExpansionTemplatePodStatus] {
	return func(_ context.Context, obj *v1beta1.ExpansionTemplatePodStatus) []reconcile.Request {
		labels := obj.GetLabels()
		name, ok := labels[v1beta1.ExpansionTemplateNameLabel]
		if !ok {
			log.Error(fmt.Errorf("expansion template status resource with no mapping label: %s", obj.GetName()), "missing label while attempting to map a expansion template status resource")
			return nil
		}
		if selfOnly {
			pod, ok := labels[v1beta1.PodLabel]
			if !ok {
				log.Error(fmt.Errorf("expansion template status resource with no pod label: %s", obj.GetName()), "missing label while attempting to map a expansion template status resource")
			}
			// Do not attempt to reconcile the resource when other pods have changed their status
			if pod != util.GetPodName() {
				return nil
			}
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name}}}
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("expansion-template-status-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to ExpansionTemplateStatus
	err = c.Watch(
		source.Kind(mgr.GetCache(), &v1beta1.ExpansionTemplatePodStatus{},
			handler.TypedEnqueueRequestsFromMapFunc(PodStatusToExpansionTemplateMapper(false)),
		))
	if err != nil {
		return err
	}

	// Watch for changes to ExpansionTemplate
	err = c.Watch(source.Kind(mgr.GetCache(), &expansionv1beta1.ExpansionTemplate{}, &handler.TypedEnqueueRequestForObject[*expansionv1beta1.ExpansionTemplate]{}))
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcileExpansionStatus{}

// ReconcileExpansionStatus reconciles an arbitrary constraint object described by Kind.
type ReconcileExpansionStatus struct {
	reader       client.Reader
	writer       client.Writer
	statusClient client.StatusClient

	scheme *runtime.Scheme
	log    logr.Logger
}

// +kubebuilder:rbac:groups=expansion.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile reads that state of the cluster for a constraint object and makes changes based on the state read
// and what is in the constraint.Spec.
func (r *ReconcileExpansionStatus) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	et := &expansionv1beta1.ExpansionTemplate{}
	err := r.reader.Get(ctx, request.NamespacedName, et)
	if err != nil {
		// If the ExpansionTemplate does not exist then we are done
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	sObjs := &v1beta1.ExpansionTemplatePodStatusList{}
	if err := r.reader.List(
		ctx,
		sObjs,
		client.MatchingLabels{v1beta1.ExpansionTemplateNameLabel: request.Name},
		client.InNamespace(util.GetNamespace()),
	); err != nil {
		return reconcile.Result{}, err
	}
	statusObjs := make(sortableStatuses, len(sObjs.Items))
	copy(statusObjs, sObjs.Items)
	sort.Sort(statusObjs)

	var s []v1beta1.ExpansionTemplatePodStatusStatus
	// created is true if at least one Pod hasn't reported any errors

	for i := range statusObjs {
		// Don't report status if it's not for the correct object. This can happen
		// if a watch gets interrupted, causing the constraint status to be deleted
		// out from underneath it
		if statusObjs[i].Status.TemplateUID != et.GetUID() {
			continue
		}
		s = append(s, statusObjs[i].Status)
	}

	et.Status.ByPod = s

	if err := r.statusClient.Status().Update(ctx, et); err != nil {
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

type sortableStatuses []v1beta1.ExpansionTemplatePodStatus

func (s sortableStatuses) Len() int {
	return len(s)
}

func (s sortableStatuses) Less(i, j int) bool {
	return s[i].Status.ID < s[j].Status.ID
}

func (s sortableStatuses) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
