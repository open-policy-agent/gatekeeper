package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutators"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	mutationschema "github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func mustParse(p string) parser.Path {
	pth, err := parser.Parse(p)
	if err != nil {
		panic(err)
	}
	return pth
}

type objectOrErr struct {
	key    client.ObjectKey
	object client.Object
	errs   []error
}

func orObject(obj client.Object) *objectOrErr {
	return &objectOrErr{
		key:    client.ObjectKeyFromObject(obj),
		object: obj,
		errs:   nil,
	}
}

func orErr(key client.ObjectKey, errs ...error) *objectOrErr {
	return &objectOrErr{
		key:    key,
		object: nil,
		errs:   errs,
	}
}

func (oe *objectOrErr) error() error {
	switch len(oe.errs) {
	case 0:
		return nil
	case 1:
		// Always return the final error
		return oe.errs[0]
	default:
		// Since there is a list of errors to return, we require different errors
		// each call. Pop the error off from the front.
		err := oe.errs[0]
		oe.errs = oe.errs[1:]
		return err
	}
}

type fakeClient struct {
	client.Client

	objects map[types.NamespacedName]*objectOrErr
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		objects: make(map[types.NamespacedName]*objectOrErr),
	}
}

var _ client.Client = &fakeClient{}

func (c *fakeClient) Get(_ context.Context, key client.ObjectKey, obj client.Object) error {
	got, found := c.objects[key]
	if !found {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	err := got.error()
	if err != nil {
		return err
	}

	switch o := obj.(type) {
	case *fakeMutatorObject:
		g, ok := got.object.(*fakeMutatorObject)
		if !ok {
			return fmt.Errorf("got.object is type %T, want %T", got.object, &fakeMutatorObject{})
		}
		*o = *g
	case *statusv1beta1.MutatorPodStatus:
		g, ok := got.object.(*statusv1beta1.MutatorPodStatus)
		if !ok {
			return fmt.Errorf("got.object is type %T, want %T", got.object, &statusv1beta1.MutatorPodStatus{})
		}
		*o = *g
	default:
		return fmt.Errorf("unrecognized type %T", obj)
	}

	return nil
}

func (c *fakeClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	original, originalExists := c.objects[toKey(obj)]
	if originalExists {
		err := original.error()
		if err != nil {
			return err
		}
	}

	c.objects[toKey(obj)] = orObject(obj)
	if originalExists {
		c.objects[toKey(obj)].errs = original.errs
	}

	return nil
}

func (c *fakeClient) Update(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
	original, originalExists := c.objects[toKey(obj)]
	if originalExists {
		err := original.error()
		if err != nil {
			return err
		}
	}

	c.objects[toKey(obj)] = orObject(obj)
	if originalExists {
		c.objects[toKey(obj)].errs = original.errs
	}

	return nil
}

func (c *fakeClient) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	original, exists := c.objects[toKey(obj)]
	if exists {
		err := original.error()
		if err != nil {
			return err
		}
	}

	delete(c.objects, toKey(obj))

	return nil
}

type fakeMutatorObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	err     error
	mutator *fakeMutator
}

var _ client.Object = &fakeMutatorObject{}

func toKey(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

func (m *fakeMutatorObject) DeepCopyObject() runtime.Object {
	mutator := m.mutator
	if mutator != nil {
		mutatorCopy := m.mutator.DeepCopy()
		var ok bool
		mutator, ok = mutatorCopy.(*fakeMutator)
		if !ok {
			panic(fmt.Errorf("got DeepCopy type %T, want %T", mutatorCopy, &fakeMutator{}))
		}
	}

	err := m.err
	if err != nil {
		err = fmt.Errorf(err.Error())
	}

	return &fakeMutatorObject{
		TypeMeta:   m.TypeMeta,
		ObjectMeta: *m.ObjectMeta.DeepCopy(),
		mutator:    mutator,
		err:        err,
	}
}

type fakeMutator struct {
	mutationtypes.Mutator

	terminal parser.NodeType
	id       mutationtypes.ID
	path     parser.Path
}

var _ mutationschema.MutatorWithSchema = &fakeMutator{}

func (m *fakeMutator) ID() mutationtypes.ID {
	return m.id
}

func (m *fakeMutator) DeepCopy() mutationtypes.Mutator {
	return &fakeMutator{
		terminal: m.terminal,
		id:       m.id,
		path:     m.path.DeepCopy(),
	}
}

func (m *fakeMutator) SchemaBindings() []schema.GroupVersionKind {
	return []schema.GroupVersionKind{rbacv1.SchemeGroupVersion.WithKind("Role")}
}

func (m *fakeMutator) TerminalType() parser.NodeType {
	return mutationschema.Unknown
}

func (m *fakeMutator) Path() parser.Path {
	return m.path
}

func (m *fakeMutator) HasDiff(right mutationtypes.Mutator) bool {
	other, ok := right.(*fakeMutator)
	if !ok {
		return true
	}

	return m.id != other.id ||
		m.terminal != other.terminal ||
		m.path.String() != other.path.String()
}

func (m *fakeMutator) UsesExternalData() bool {
	return false
}

type errSome struct{ id int }

func newErrSome(id int) error { return &errSome{id: id} }

func (e *errSome) Is(target error) bool {
	if t, ok := target.(*errSome); ok {
		return t.id == e.id
	}

	return false
}

func (e *errSome) Error() string {
	return fmt.Sprintf("some error: %d", e.id)
}

func newFakeReconciler(t *testing.T, c client.Client, events chan event.GenericEvent) *Reconciler {
	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	const podName = "no-pod"

	return &Reconciler{
		Client:         c,
		log:            logr.New(logf.NullLogSink{}),
		newMutationObj: func() client.Object { return &fakeMutatorObject{} },
		cache:          mutators.NewMutationCache(),
		mutatorFor: func(object client.Object) (mutationtypes.Mutator, error) {
			fake, ok := object.(*fakeMutatorObject)
			if !ok {
				return nil, fmt.Errorf("called mutatorFor(%T), want mutatorFor(%T)",
					object, &fakeMutatorObject{})
			}

			if fake.err != nil {
				return nil, fake.err
			}

			if fake.mutator == nil {
				return nil, nil
			}

			fake.mutator.id = mutationtypes.MakeID(object)
			return fake.mutator, nil
		},
		system: mutation.NewSystem(mutation.SystemOpts{}),
		getPod: func(ctx context.Context) (*corev1.Pod, error) {
			return fakes.Pod(
				fakes.WithNamespace("gatekeeper-system"),
				fakes.WithName(podName),
				// These tests do not depend on UID and validating this is annoying.
				fakes.WithUID(""),
			), nil
		},
		scheme: s,
		gvk:    mutationsv1alpha1.GroupVersion.WithKind("fake"),
		events: events,
	}
}

type fakeEvents struct {
	channel chan event.GenericEvent

	wg sync.WaitGroup

	eventsMtx sync.Mutex
	events    mutationschema.IDSet
}

func (e *fakeEvents) add(id mutationtypes.ID) {
	e.eventsMtx.Lock()
	e.events[id] = true
	e.eventsMtx.Unlock()
}

func newFakeEvents() *fakeEvents {
	result := &fakeEvents{
		channel: make(chan event.GenericEvent),
		events:  make(mutationschema.IDSet),
	}

	result.wg.Add(1)
	go func() {
		for e := range result.channel {
			result.add(mutationtypes.MakeID(e.Object))
		}
		result.wg.Done()
	}()

	return result
}

// Get closes the events channel, waits for all events to be inserted in the map,
// and then returns the resulting map.
// Safe to call multiple times.
func (e *fakeEvents) Get() mutationschema.IDSet {
	close(e.channel)
	e.wg.Wait()

	return e.events
}

func now() *metav1.Time {
	t := metav1.NewTime(time.Now())
	return &t
}

func TestReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name string
		// before are added to the Client and Reconciler before the test begins. The
		// outcomes of adding these are not evaluated.
		before  []*objectOrErr
		request *objectOrErr

		want       *statusv1beta1.MutatorPodStatus
		wantErr    error
		wantEvents mutationschema.IDSet
	}{
		{
			name:   "create Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			},
			wantErr: nil,
		},
		{
			name: "replace Mutator",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			},
			wantErr: nil,
		},
		{
			name: "delete Mutator",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
				mutator:    &fakeMutator{},
			}),
			want:    nil,
			wantErr: nil,
		},
		{
			name:   "delete nonexistent Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
				mutator:    &fakeMutator{},
			}),
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "error getting Mutator",
			before:  nil,
			request: orErr(client.ObjectKey{Name: "bar"}, newErrSome(1)),
			wantErr: newErrSome(1),
		},
		{
			name:   "error instantiating Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				err:        newErrSome(1),
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   false,
					Errors:     []statusv1beta1.MutatorError{{Message: newErrSome(1).Error()}},
				},
			},
			wantErr: nil,
		},
		{
			name: "error creating Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					apierrors.NewNotFound(schema.GroupResource{}, "no--pod-fake-bar"), newErrSome(1)),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: newErrSome(1),
		},
		{
			name:   "error upserting Mutator into system",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    nil,
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{
						{
							Message: mutationschema.ErrNilMutator.Error(),
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "error getting Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"}, newErrSome(1)),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: newErrSome(1),
		},
		{
			name: "error updating Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					apierrors.NewNotFound(schema.GroupResource{}, "no--pod-fake-bar"), nil, newErrSome(1)),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: newErrSome(1),
		},
		{
			name: "error deleting Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					newErrSome(1)),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
				mutator:    &fakeMutator{},
			}),
			wantErr: newErrSome(1),
		},
		{
			name: "error updating Mutator and Mutator status",
			before: []*objectOrErr{
				{
					object: &fakeMutatorObject{
						TypeMeta:   metav1.TypeMeta{Kind: "fake"},
						ObjectMeta: metav1.ObjectMeta{Name: "bar"},
						mutator:    &fakeMutator{},
					},
					errs: []error{nil, newErrSome(1)},
				},
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					apierrors.NewNotFound(schema.GroupResource{}, "no--pod-fake-bar"), nil, newErrSome(2)),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				err:        newErrSome(1),
			}),
			wantErr: newErrSome(2),
		},
		{
			name: "errors on inserted conflicting mutator",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar"),
				},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar--2",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar-2",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{
						{
							Type: mutationschema.ErrConflictingSchemaType,
							Message: mutationschema.ErrConflictingSchema{Conflicts: mutationschema.IDSet{
								{Kind: "fake", Name: "bar-1"}: true,
								{Kind: "fake", Name: "bar-2"}: true,
							}}.Error(),
						},
					},
				},
			},
			wantEvents: map[mutationtypes.ID]bool{{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar-1"}: true},
			wantErr:    nil,
		},
		{
			name: "fix conflicting mutator",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar"),
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].qux"),
				},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar--1",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar-1",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			},
			wantEvents: map[mutationtypes.ID]bool{{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar-1"}: true},
			wantErr:    nil,
		},
		{
			name: "keep errors on deleted conflicting mutator",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar.qux"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-3"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar[name: foo].qux"),
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-1", DeletionTimestamp: now()},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}),
			want:    nil,
			wantErr: nil,
			wantEvents: map[mutationtypes.ID]bool{
				{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar-1"}: true,
				{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar-2"}: true,
				{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar-3"}: true,
			},
		},
		{
			name: "fix mutator error",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}), orObject(&statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{
						{Type: mutationschema.ErrConflictingSchemaType},
					},
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			},
			wantErr: nil,
		},
		{
			name: "add mutator error",
			before: []*objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec.foo.bar"),
				},
			}), orObject(&statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}),
			want: &statusv1beta1.MutatorPodStatus{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "gatekeeper-system",
					Name:      "no--pod-fake-bar",
					Labels: map[string]string{
						"internal.gatekeeper.sh/mutator-kind": "fake",
						"internal.gatekeeper.sh/mutator-name": "bar",
						"internal.gatekeeper.sh/pod":          "no-pod",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
				},
				Status: statusv1beta1.MutatorPodStatusStatus{
					ID:         "no-pod",
					Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{{
						Type: mutationschema.ErrConflictingSchemaType,
						Message: mutationschema.NewErrConflictingSchema(mutationschema.IDSet{
							{Kind: "fake", Name: "bar"}:   true,
							{Kind: "fake", Name: "bar-1"}: true,
						}).Error(),
					}},
				},
			},
			wantErr:    nil,
			wantEvents: map[mutationtypes.ID]bool{{Kind: "fake", Group: statusv1beta1.MutationsGroup, Name: "bar"}: true},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient()
			events := newFakeEvents()
			r := newFakeReconciler(t, c, events.channel)

			ctx := context.Background()

			for _, obj := range tc.before {
				c.objects[obj.key] = obj

				if len(obj.errs) > 0 {
					continue
				}

				if m, isMutator := obj.object.(*fakeMutatorObject); !isMutator || m.mutator == nil {
					continue
				}

				_, _ = r.Reconcile(ctx, reconcile.Request{
					NamespacedName: obj.key,
				})
			}

			c.objects[tc.request.key] = tc.request

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: tc.request.key})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got Reconcile() error = %v, want %v", err, tc.wantErr)
			}

			if tc.want != nil {
				got, ok := c.objects[toKey(tc.want)]
				if !ok {
					t.Fatalf("got %v does not exist, want exist", toKey(tc.want))
				}

				if diff := cmp.Diff(tc.want, got.object); diff != "" {
					t.Error(diff)
				}
			}

			gotEvents := events.Get()
			if diff := cmp.Diff(tc.wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestReconciler_Reconcile_DeletePodStatus(t *testing.T) {
	c := newFakeClient()
	events := newFakeEvents()
	r := newFakeReconciler(t, c, events.channel)

	ctx := context.Background()

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar"},
		mutator:    &fakeMutator{},
	}
	c.objects[toKey(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: toKey(m)})
	if err != nil {
		t.Fatal(err)
	}

	statusKey := client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"}
	gotStatus, ok := c.objects[statusKey]
	if !ok {
		t.Fatalf("could not find %v", statusKey)
	}

	wantStatus := &statusv1beta1.MutatorPodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "gatekeeper-system",
			Name:      "no--pod-fake-bar",
			Labels: map[string]string{
				"internal.gatekeeper.sh/mutator-kind": "fake",
				"internal.gatekeeper.sh/mutator-name": "bar",
				"internal.gatekeeper.sh/pod":          "no-pod",
			},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "no-pod"}},
		},
		Status: statusv1beta1.MutatorPodStatusStatus{
			ID:         "no-pod",
			Operations: []string{"audit", "mutation-status", "mutation-webhook", "status", "webhook"},
			Enforced:   true,
		},
	}
	if diff := cmp.Diff(wantStatus, gotStatus.object); diff != "" {
		t.Fatal(diff)
	}

	m.DeletionTimestamp = now()

	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: toKey(m)})
	if err != nil {
		t.Fatal(err)
	}

	_, ok = c.objects[statusKey]
	if ok {
		t.Fatalf("got %v exists, want not exists", statusKey)
	}

	var wantEvents mutationschema.IDSet
	gotEvents := events.Get()
	if diff := cmp.Diff(wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}

func TestReconcile_AlreadyDeleting(t *testing.T) {
	c := newFakeClient()
	events := newFakeEvents()
	r := newFakeReconciler(t, c, events.channel)

	ctx := context.Background()

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
		mutator:    &fakeMutator{},
	}
	c.objects[toKey(m)] = orObject(m)

	status := &statusv1beta1.MutatorPodStatus{
		ObjectMeta: metav1.ObjectMeta{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
	}
	c.objects[toKey(status)] = orObject(status)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: toKey(m)})
	if err != nil {
		t.Fatal(err)
	}

	_, gotFound := c.objects[toKey(status)]
	if gotFound {
		t.Errorf("got %v still exists after Mutator deleted, want not found",
			toKey(status))
	}

	var wantEvents mutationschema.IDSet
	gotEvents := events.Get()
	if diff := cmp.Diff(wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}

func TestReconcile_AlreadyDeleted(t *testing.T) {
	c := newFakeClient()
	events := newFakeEvents()
	r := newFakeReconciler(t, c, events.channel)

	ctx := context.Background()

	status := &statusv1beta1.MutatorPodStatus{
		ObjectMeta: metav1.ObjectMeta{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
	}
	c.objects[toKey(status)] = orObject(status)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "bar"}})
	if err != nil {
		t.Fatal(err)
	}

	_, gotFound := c.objects[toKey(status)]
	if gotFound {
		t.Errorf("got %v still exists after Mutator deleted, want not found",
			toKey(status))
	}

	var wantEvents mutationschema.IDSet
	gotEvents := events.Get()
	if diff := cmp.Diff(wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}

func TestReconcile_ReconcileUpsert_GetPodError(t *testing.T) {
	c := newFakeClient()
	events := newFakeEvents()
	r := newFakeReconciler(t, c, events.channel)

	ctx := context.Background()

	r.getPod = func(ctx context.Context) (*corev1.Pod, error) {
		return nil, newErrSome(1)
	}

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar"},
		mutator:    &fakeMutator{},
	}
	c.objects[toKey(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: toKey(m)})
	if !errors.Is(err, newErrSome(1)) {
		t.Errorf("got Reconcile() error = %v, want %v", err, newErrSome(1))
	}

	var wantEvents mutationschema.IDSet
	gotEvents := events.Get()
	if diff := cmp.Diff(wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}

func TestReconcile_ReconcileDeleted_GetPodError(t *testing.T) {
	c := newFakeClient()
	events := newFakeEvents()
	r := newFakeReconciler(t, c, events.channel)

	ctx := context.Background()

	r.getPod = func(ctx context.Context) (*corev1.Pod, error) {
		return nil, newErrSome(1)
	}

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
		mutator:    &fakeMutator{},
	}
	c.objects[toKey(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: toKey(m)})
	if !errors.Is(err, newErrSome(1)) {
		t.Errorf("got Reconcile() error = %v, want %v", err, newErrSome(1))
	}

	var wantEvents mutationschema.IDSet
	gotEvents := events.Get()
	if diff := cmp.Diff(wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
		t.Error(diff)
	}
}
