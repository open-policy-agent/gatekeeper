package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutators"
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
	err    error
}

func orObject(obj client.Object) objectOrErr {
	return objectOrErr{
		key:    client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()},
		object: obj,
		err:    nil,
	}
}

func orErr(key client.ObjectKey, err error) objectOrErr {
	return objectOrErr{
		key:    key,
		object: nil,
		err:    err,
	}
}

type fakeClient struct {
	client.Client

	objects map[types.NamespacedName]objectOrErr
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		objects: make(map[types.NamespacedName]objectOrErr),
	}
}

var _ client.Client = &fakeClient{}

func (c *fakeClient) Get(_ context.Context, key client.ObjectKey, obj client.Object) error {
	got, found := c.objects[key]
	if !found {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	if got.err != nil {
		return got.err
	}

	switch o := obj.(type) {
	case *fakeMutatorObject:
		*o = *got.object.(*fakeMutatorObject)
	case *statusv1beta1.MutatorPodStatus:
		*o = *got.object.(*statusv1beta1.MutatorPodStatus)
	default:
		panic(fmt.Errorf("unrecognized type %T", obj))
	}

	return nil
}

func (c *fakeClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	if original, exists := c.objects[key(obj)]; exists {
		if original.err != nil {
			return original.err
		}
	}

	c.objects[key(obj)] = orObject(obj)

	return nil
}

func (c *fakeClient) Update(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
	if original, exists := c.objects[key(obj)]; exists {
		if original.err != nil {
			return original.err
		}
	}

	c.objects[key(obj)] = orObject(obj)

	return nil
}

func (c *fakeClient) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	delete(c.objects, key(obj))

	return nil
}

type fakeMutatorObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	err     error
	mutator *fakeMutator
}

var _ client.Object = &fakeMutatorObject{}

func key(obj client.Object) types.NamespacedName {
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
	x := *m
	return &x
}

func (m *fakeMutator) SchemaBindings() []schema.GroupVersionKind {
	return []schema.GroupVersionKind{rbacv1.SchemeGroupVersion.WithKind("Role")}
}

func (m *fakeMutator) TerminalType() parser.NodeType {
	n := len(m.path.Nodes) - 1
	if n < 0 {
		return ""
	}
	return m.path.Nodes[n].Type()
}

func (m *fakeMutator) Path() parser.Path {
	return m.path
}

func (m *fakeMutator) HasDiff(right mutationtypes.Mutator) bool {
	other, ok := right.(*fakeMutator)
	if !ok {
		return true
	}

	return m.id == other.id &&
		m.terminal == other.terminal &&
		m.path.String() == other.path.String()
}

type fakeLogger struct {
	logr.Logger
}

func (l fakeLogger) Info(string, ...interface{}) {}

func (l fakeLogger) Error(error, string, ...interface{}) {}

var errSome = errors.New("some error")

func newFakeReconciler(c client.Client) *Reconciler {
	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	if err != nil {
		panic(err)
	}

	const podName = "no-pod"

	return &Reconciler{
		Client:         c,
		log:            fakeLogger{},
		newMutationObj: func() client.Object { return &fakeMutatorObject{} },
		cache:          mutators.NewMutationCache(),
		mutatorFor: func(object client.Object) (mutationtypes.Mutator, error) {
			fake, ok := object.(*fakeMutatorObject)
			if !ok {
				return nil, fmt.Errorf("called mutatorFor(%T), want mutatorFor(%T)", object, &fakeMutatorObject{})
			}

			if fake.err != nil {
				return nil, fake.err
			}

			fake.mutator.id = mutationtypes.MakeID(object)
			return fake.mutator, nil
		},
		system: mutation.NewSystem(mutation.SystemOpts{}),
		getPod: func(ctx context.Context) (*corev1.Pod, error) {
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: "gatekeeper-system"}}, nil
		},
		scheme: s,
		podOwnershipMode: statusv1beta1.PodOwnershipEnabled,
	}
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
		before  []objectOrErr
		request objectOrErr

		want    []*statusv1beta1.MutatorPodStatus
		wantErr error
	}{
		{
			name:   "create Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			}},
			wantErr: nil,
		},
		{
			name: "replace Mutator",
			before: []objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				mutator:    &fakeMutator{},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   true,
					Errors:     nil,
				},
			}},
			wantErr: nil,
		},
		{
			name: "delete Mutator",
			before: []objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				mutator:    &fakeMutator{},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar", DeletionTimestamp: now()},
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
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar", DeletionTimestamp: now()},
				mutator:    &fakeMutator{},
			}),
			want:    nil,
			wantErr: nil,
		},
		{
			name:    "error getting Mutator",
			before:  nil,
			request: orErr(client.ObjectKey{Namespace: "foo", Name: "bar"}, errSome),
			wantErr: errSome,
		},
		{
			name:   "error instantiating Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				err:        errSome,
			}),
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   false,
					Errors:     []statusv1beta1.MutatorError{{Message: errSome.Error()}},
				},
			}},
			wantErr: nil,
		},
		{
			name: "error updating Mutator status",
			before: []objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"}, errSome),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: errSome,
		},
		{
			name: "errors on inserted conflicting mutator",
			before: []objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar"),
				},
			}),
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{{Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
						{Kind: "fake", Namespace: "foo", Name: "bar-1"},
						{Kind: "fake", Namespace: "foo", Name: "bar-2"},
					}}.Error()}},
				},
			}, {
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{{Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
						{Kind: "fake", Namespace: "foo", Name: "bar-1"},
						{Kind: "fake", Namespace: "foo", Name: "bar-2"},
					}}.Error()}},
				},
			}},
			wantErr: nil,
		},
		{
			name: "keep errors on deleted conflicting mutator",
			before: []objectOrErr{orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-1"},
				mutator: &fakeMutator{
					path: mustParse("spec[name: foo].bar"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-2"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar.qux"),
				},
			}), orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-3"},
				mutator: &fakeMutator{
					path: mustParse("spec.bar[name: foo].qux"),
				},
			})},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-3", DeletionTimestamp: now()},
				mutator: &fakeMutator{
					path: mustParse("spec.bar[name: foo].qux"),
				},
			}),
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{{Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
						{Kind: "fake", Namespace: "foo", Name: "bar-1"},
						{Kind: "fake", Namespace: "foo", Name: "bar-2"},
					}}.Error()}},
				},
			}, {
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
					Enforced:   false,
					Errors: []statusv1beta1.MutatorError{{Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
						{Kind: "fake", Namespace: "foo", Name: "bar-1"},
						{Kind: "fake", Namespace: "foo", Name: "bar-2"},
					}}.Error()}},
				},
			}},
			wantErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient()
			r := newFakeReconciler(c)

			ctx := context.Background()

			for _, m := range tc.before {
				c.objects[m.key] = m

				if m.err != nil {
					continue
				}

				if m, isMutator := m.object.(*fakeMutatorObject); !isMutator || m.mutator == nil {
					continue
				}

				_, _ = r.Reconcile(ctx, reconcile.Request{
					NamespacedName: m.key,
				})
			}

			c.objects[tc.request.key] = tc.request

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: tc.request.key})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got Reconcile() error = %v, want %v", err, tc.wantErr)
			}

			for _, want := range tc.want {
				got, ok := c.objects[key(want)]
				if !ok {
					t.Errorf("want %v to exist, got not exist", key(want))
					continue
				}

				if diff := cmp.Diff(want, got.object); diff != "" {
					t.Error(diff)
				}
			}
		})
	}
}
