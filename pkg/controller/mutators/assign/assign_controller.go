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

package assign

import (
	"context"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutators/core"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// eventQueueSize is how many events to queue before blocking.
const eventQueueSize = 1024

type Adder struct {
	MutationSystem *mutation.System
	Tracker        *readiness.Tracker
	GetPod         func(context.Context) (*corev1.Pod, error)
}

// Add creates a new Assign Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	events := make(chan event.GenericEvent, eventQueueSize)

	adder := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "Assign",
		NewMutationObj: func() client.Object { return &mutationsv1alpha1.Assign{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assign := obj.(*mutationsv1alpha1.Assign) // nolint:forcetypeassert
			return mutators.MutatorForAssign(assign)
		},
		Events: events,
	}
	return adder.Add(mgr)
}

func (a *Adder) InjectOpa(o *opa.Client) {}

func (a *Adder) InjectWatchManager(w *watch.Manager) {}

func (a *Adder) InjectControllerSwitch(cs *watch.ControllerSwitch) {}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

func (a *Adder) InjectMutationSystem(mutationSystem *mutation.System) {
	a.MutationSystem = mutationSystem
}
