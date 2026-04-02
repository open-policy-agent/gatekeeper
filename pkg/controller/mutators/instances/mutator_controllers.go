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
	apiTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	scheme := mgr.GetScheme()

	// Per-controller channels for fan-out of conflict events.
	assignCh := make(chan event.GenericEvent, eventQueueSize)
	modifySetCh := make(chan event.GenericEvent, eventQueueSize)
	assignImageCh := make(chan event.GenericEvent, eventQueueSize)

	// Fan-out: broadcast every conflict event to all per-controller channels.
	go func() {
		for evt := range events {
			assignCh <- evt
			modifySetCh <- evt
			assignImageCh <- evt
		}
	}()

	// kindFilterSource creates a source.Channel with a handler that only enqueues
	// events matching the specified kind, discarding the rest.
	kindFilterSource := func(ch <-chan event.GenericEvent, kind string) source.Source {
		return source.Channel(ch,
			handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				if obj.GetObjectKind().GroupVersionKind().Kind != kind {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: apiTypes.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      obj.GetName(),
					},
				}}
			}))
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
		EventsSource: kindFilterSource(assignCh, "Assign"),
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
		EventsSource: kindFilterSource(modifySetCh, "ModifySet"),
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
		EventsSource: kindFilterSource(assignImageCh, "AssignImage"),
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
