package instances

import (
	"context"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	mutationsv1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1alpha1"
	ctrlmutators "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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

func routeConflictEvents(ctx context.Context, events <-chan event.GenericEvent, assignCh, modifySetCh, assignImageCh chan<- event.GenericEvent) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-events:
			if !ok {
				return nil
			}

			var ch chan<- event.GenericEvent
			switch evt.Object.GetObjectKind().GroupVersionKind().Kind {
			case "Assign":
				ch = assignCh
			case "ModifySet":
				ch = modifySetCh
			case "AssignImage":
				ch = assignImageCh
			default:
				continue
			}

			select {
			case <-ctx.Done():
				return nil
			case ch <- evt:
			}
		}
	}
}

// Add creates all mutation controllers and adds them to the manager.
func (a *Adder) Add(mgr manager.Manager) error {
	// events is shared across all mutators that can affect the implied schema
	// of kinds to be mutated, since these mutators can set each other into conflict
	events := make(chan event.GenericEvent, eventQueueSize)
	scheme := mgr.GetScheme()

	// Per-controller channels for fan-out of conflict events.
	assignCh := make(chan event.GenericEvent, eventQueueSize)
	modifySetCh := make(chan event.GenericEvent, eventQueueSize)
	assignImageCh := make(chan event.GenericEvent, eventQueueSize)

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return routeConflictEvents(ctx, events, assignCh, modifySetCh, assignImageCh)
	})); err != nil {
		return err
	}

	reporter := ctrlmutators.NewStatsReporter()

	assign := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "Assign",
		NewMutationObj: func() client.Object { return &mutationsv1.Assign{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assign := obj.(*mutationsv1.Assign) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.Assign{}
			if err := scheme.Convert(assign, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForAssign(unversioned)
		},
		Events:       events,
		EventsSource: source.Channel(assignCh, &handler.EnqueueRequestForObject{}),
		Reporter:     reporter,
	}
	if err := assign.Add(mgr); err != nil {
		return err
	}

	modifySet := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "ModifySet",
		NewMutationObj: func() client.Object { return &mutationsv1.ModifySet{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			modifyset := obj.(*mutationsv1.ModifySet) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.ModifySet{}
			if err := scheme.Convert(modifyset, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForModifySet(unversioned)
		},
		Events:       events,
		EventsSource: source.Channel(modifySetCh, &handler.EnqueueRequestForObject{}),
		Reporter:     reporter,
	}
	if err := modifySet.Add(mgr); err != nil {
		return err
	}

	assignImage := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "AssignImage",
		NewMutationObj: func() client.Object { return &v1alpha1.AssignImage{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assignImage := obj.(*v1alpha1.AssignImage) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.AssignImage{}
			if err := scheme.Convert(assignImage, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForAssignImage(unversioned)
		},
		Events:       events,
		EventsSource: source.Channel(assignImageCh, &handler.EnqueueRequestForObject{}),
		Reporter:     reporter,
	}
	if err := assignImage.Add(mgr); err != nil {
		return err
	}

	assignMetadata := core.Adder{
		Tracker:        a.Tracker,
		GetPod:         a.GetPod,
		MutationSystem: a.MutationSystem,
		Kind:           "AssignMetadata",
		NewMutationObj: func() client.Object { return &mutationsv1.AssignMetadata{} },
		MutatorFor: func(obj client.Object) (types.Mutator, error) {
			// The type is provided by the `NewObj` function above. If we
			// are fed the wrong type, this is a non-recoverable error and we
			// may as well crash for visibility
			assignMeta := obj.(*mutationsv1.AssignMetadata) // nolint:forcetypeassert
			unversioned := &mutationsunversioned.AssignMetadata{}
			if err := scheme.Convert(assignMeta, unversioned, nil); err != nil {
				return nil, err
			}
			return mutators.MutatorForAssignMetadata(unversioned)
		},
		Reporter: reporter,
	}
	return assignMetadata.Add(mgr)
}

func (a *Adder) InjectTracker(t *readiness.Tracker) {
	a.Tracker = t
}

func (a *Adder) InjectGetPod(getPod func(ctx context.Context) (*corev1.Pod, error)) {
	a.GetPod = getPod
}

func (a *Adder) InjectMutationSystem(mutationSystem *mutation.System) {
	a.MutationSystem = mutationSystem
}
