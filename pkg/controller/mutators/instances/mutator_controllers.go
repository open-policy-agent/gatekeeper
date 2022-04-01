package instances

import (
	"context"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1beta1"
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
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// eventQueueSize is how many events to queue before blocking.
const eventQueueSize = 1024

type Adder struct {
	MutationSystem *mutation.System
	Tracker        *readiness.Tracker
	GetPod         func(context.Context) (*corev1.Pod, error)
}

// Add creates all mutation controllers and adds them to the manager.
func (a *Adder) Add(mgr manager.Manager) error {
	// events is shared across all mutators that can affect the implied schema
	// of kinds to be mutated, since these mutators can set each other into conflict
	events := make(chan event.GenericEvent, eventQueueSize)
	eventsSource := &source.Channel{Source: events, DestBufferSize: 1024}
	scheme := mgr.GetScheme()

	assign := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "Assign",
		NewMutationObj: func() client.Object { return &mutationsv1beta1.Assign{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assign := obj.(*mutationsv1beta1.Assign) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.Assign{}
			if err := scheme.Convert(assign, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForAssign(unversioned)
		},
		Events:       events,
		EventsSource: eventsSource,
	}
	if err := assign.Add(mgr); err != nil {
		return err
	}

	modifySet := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "ModifySet",
		NewMutationObj: func() client.Object { return &mutationsv1beta1.ModifySet{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			modifyset := obj.(*mutationsv1beta1.ModifySet) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.ModifySet{}
			if err := scheme.Convert(modifyset, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForModifySet(unversioned)
		},
		Events:       events,
		EventsSource: eventsSource,
	}

	if err := modifySet.Add(mgr); err != nil {
		return err
	}

	assignMetadata := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "AssignMetadata",
		NewMutationObj: func() client.Object { return &mutationsv1beta1.AssignMetadata{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assignMeta := obj.(*mutationsv1beta1.AssignMetadata) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.AssignMetadata{}
			if err := scheme.Convert(assignMeta, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForAssignMetadata(unversioned)
		},
	}
	return assignMetadata.Add(mgr)
}

func (a *Adder) InjectOpa(o *constraintclient.Client) {}

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

func (a *Adder) InjectProviderCache(providerCache *externaldata.ProviderCache) {}
