package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
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
	errs   []error
}

func orObject(obj client.Object) *objectOrErr {
	return &objectOrErr{
		key:    client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()},
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
		*o = *got.object.(*fakeMutatorObject)
	case *statusv1beta1.MutatorPodStatus:
		*o = *got.object.(*statusv1beta1.MutatorPodStatus)
	default:
		return fmt.Errorf("unrecognized type %T", obj)
	}

	return nil
}

func (c *fakeClient) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	original, originalExists := c.objects[key(obj)]
	if originalExists {
		err := original.error()
		if err != nil {
			return err
		}
	}

	c.objects[key(obj)] = orObject(obj)
	if originalExists {
		c.objects[key(obj)].errs = original.errs
	}

	return nil
}

func (c *fakeClient) Update(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
	original, originalExists := c.objects[key(obj)]
	if originalExists {
		err := original.error()
		if err != nil {
			return err
		}
	}

	c.objects[key(obj)] = orObject(obj)
	if originalExists {
		c.objects[key(obj)].errs = original.errs
	}

	return nil
}

func (c *fakeClient) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	original, exists := c.objects[key(obj)]
	if exists {
		err := original.error()
		if err != nil {
			return err
		}
	}

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

type noOpLogger struct {
	logr.Logger
}

func (l noOpLogger) Info(string, ...interface{}) {}

func (l noOpLogger) Error(error, string, ...interface{}) {}

var errSome = errors.New("some error")

func newFakeReconciler(t *testing.T, c client.Client) *Reconciler {
	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	const podName = "no-pod"

	return &Reconciler{
		Client:         c,
		log:            noOpLogger{},
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

			fake.mutator.id = mutationtypes.MakeID(object)
			return fake.mutator, nil
		},
		system: mutation.NewSystem(mutation.SystemOpts{}),
		getPod: func(ctx context.Context) (*corev1.Pod, error) {
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: "gatekeeper-system"}}, nil
		},
		scheme:           s,
		podOwnershipMode: statusv1beta1.PodOwnershipEnabled,
		gvk:              mutationsv1alpha1.GroupVersion.WithKind("fake"),
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
		before  []*objectOrErr
		request *objectOrErr

		want    []*statusv1beta1.MutatorPodStatus
		wantErr error
	}{
		{
			name:   "create Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
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
			request: orErr(client.ObjectKey{Name: "bar"}, errSome),
			wantErr: errSome,
		},
		{
			name:   "error instantiating Mutator",
			before: nil,
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
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
			name: "error creating Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					apierrors.NewNotFound(schema.GroupResource{}, "no--pod-fake-bar"), errSome),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: errSome,
		},
		{
			name: "error getting Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"}, errSome),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: errSome,
		},
		{
			name: "error updating Mutator status",
			before: []*objectOrErr{
				orErr(client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
					apierrors.NewNotFound(schema.GroupResource{}, "no--pod-fake-bar"), nil, errSome),
			},
			request: orObject(&fakeMutatorObject{
				TypeMeta:   metav1.TypeMeta{Kind: "fake"},
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
				mutator:    &fakeMutator{},
			}),
			wantErr: errSome,
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
					Errors: []statusv1beta1.MutatorError{{
						Type: mutationschema.ErrConflictingSchemaType,
						Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
							{Kind: "fake", Name: "bar-1"},
							{Kind: "fake", Name: "bar-2"},
						}}.Error(),
					}},
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
					Errors: []statusv1beta1.MutatorError{{
						Type: mutationschema.ErrConflictingSchemaType,
						Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
							{Kind: "fake", Name: "bar-1"},
							{Kind: "fake", Name: "bar-2"},
						}}.Error(),
					}},
				},
			}},
			wantErr: nil,
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
					Enforced:   true,
					Errors:     nil,
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
					Enforced:   true,
					Errors:     nil,
				},
			}},
			wantErr: nil,
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
			want: []*statusv1beta1.MutatorPodStatus{{
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
					Errors: []statusv1beta1.MutatorError{{
						Type: mutationschema.ErrConflictingSchemaType,
						Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
							{Kind: "fake", Name: "bar-2"},
							{Kind: "fake", Name: "bar-3"},
						}}.Error(),
					}},
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
					Errors: []statusv1beta1.MutatorError{{
						Type: mutationschema.ErrConflictingSchemaType,
						Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
							{Kind: "fake", Name: "bar-2"},
							{Kind: "fake", Name: "bar-3"},
						}}.Error(),
					}},
				},
			}},
			wantErr: nil,
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
					Operations: []string{"audit", "mutation-status", "status", "webhook"},
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient()
			r := newFakeReconciler(t, c)

			ctx := context.Background()

			for _, m := range tc.before {
				c.objects[m.key] = m

				if len(m.errs) > 0 {
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

func TestReconciler_Reconcile_DeletePodStatus(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar"},
		mutator:    &fakeMutator{},
	}
	c.objects[key(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m)})
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
			Operations: []string{"audit", "mutation-status", "status", "webhook"},
			Enforced:   true,
		},
	}
	if diff := cmp.Diff(wantStatus, gotStatus.object); diff != "" {
		t.Fatal(diff)
	}

	m.DeletionTimestamp = now()

	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m)})
	if err != nil {
		t.Fatal(err)
	}

	_, ok = c.objects[statusKey]
	if ok {
		t.Fatalf("got %v exists, want not exists", statusKey)
	}
}

func TestReconciler_Reconcile_PreserveError(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	// Introduce two conflicting mutators.
	m1 := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar-1"},
		mutator: &fakeMutator{
			path: mustParse("spec.foo"),
		},
	}
	c.objects[key(m1)] = orObject(m1)

	m2 := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
		mutator: &fakeMutator{
			path: mustParse("spec[name: bar].foo"),
		},
	}
	c.objects[key(m2)] = orObject(m2)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m1)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m2)})
	if err != nil {
		t.Fatal(err)
	}

	// Modify m2 to have an error.
	m2b := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar-2"},
		err:        errSome,
	}
	c.objects[key(m2)] = orObject(m2b)
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m2)})
	if err != nil {
		t.Fatal(err)
	}

	statusKey1 := client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar--1"}
	gotStatus1, ok := c.objects[statusKey1]
	if !ok {
		t.Fatalf("could not find %v", statusKey1)
	}

	// Ensure the Pod Statuses look like we want.
	wantStatus1 := &statusv1beta1.MutatorPodStatus{
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
			Errors: []statusv1beta1.MutatorError{{
				Type: mutationschema.ErrConflictingSchemaType,
				Message: mutationschema.ErrConflictingSchema{Conflicts: []mutationtypes.ID{
					{Kind: "fake", Name: "bar-1"},
					{Kind: "fake", Name: "bar-2"},
				}}.Error(),
			}},
			Enforced: false,
		},
	}
	if diff := cmp.Diff(wantStatus1, gotStatus1.object); diff != "" {
		t.Fatal(diff)
	}

	wantStatus2 := &statusv1beta1.MutatorPodStatus{
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
			Errors:     []statusv1beta1.MutatorError{{Message: errSome.Error()}},
			Enforced:   false,
		},
	}

	statusKey2 := client.ObjectKey{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar--2"}
	gotStatus2, ok := c.objects[statusKey2]
	if !ok {
		t.Fatalf("could not find %v", statusKey2)
	}
	if diff := cmp.Diff(wantStatus2, gotStatus2.object); diff != "" {
		t.Fatal(diff)
	}

	// Remove m1. We expect m2 to still show an error.
	m1.DeletionTimestamp = now()
	c.objects[key(m1)] = orObject(m1)
	_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m1)})
	if err != nil {
		t.Fatal(err)
	}

	_, ok = c.objects[statusKey1]
	if ok {
		t.Fatalf("got PodStatus for first mutator: %v", statusKey1)
	}

	gotStatus2, ok = c.objects[statusKey2]
	if !ok {
		t.Fatalf("could not find %v", statusKey2)
	}
	if diff := cmp.Diff(wantStatus2, gotStatus2.object); diff != "" {
		t.Fatal(diff)
	}
}

func TestReconcile_AlreadyDeleting(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
		mutator:    &fakeMutator{},
	}
	c.objects[key(m)] = orObject(m)

	status := &statusv1beta1.MutatorPodStatus{
		ObjectMeta: metav1.ObjectMeta{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
	}
	c.objects[key(status)] = orObject(status)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m)})
	if err != nil {
		t.Fatal(err)
	}

	_, gotFound := c.objects[key(status)]
	if gotFound {
		t.Errorf("got %v still exists after Mutator deleted, want not found",
			key(status))
	}
}

func TestReconcile_AlreadyDeleted(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	status := &statusv1beta1.MutatorPodStatus{
		ObjectMeta: metav1.ObjectMeta{Namespace: "gatekeeper-system", Name: "no--pod-fake-bar"},
	}
	c.objects[key(status)] = orObject(status)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "bar"}})
	if err != nil {
		t.Fatal(err)
	}

	_, gotFound := c.objects[key(status)]
	if gotFound {
		t.Errorf("got %v still exists after Mutator deleted, want not found",
			key(status))
	}
}

func TestReconcile_ReconcileUpsert_GetPodError(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	r.getPod = func(ctx context.Context) (*corev1.Pod, error) {
		return nil, errSome
	}

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar"},
		mutator:    &fakeMutator{},
	}
	c.objects[key(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m)})
	if !errors.Is(err, errSome) {
		t.Errorf("got Reconcile() error = %v, want %v", err, errSome)
	}
}

func TestReconcile_ReconcileDeleted_GetPodError(t *testing.T) {
	c := newFakeClient()
	r := newFakeReconciler(t, c)

	ctx := context.Background()

	r.getPod = func(ctx context.Context) (*corev1.Pod, error) {
		return nil, errSome
	}

	m := &fakeMutatorObject{
		TypeMeta:   metav1.TypeMeta{Kind: "fake"},
		ObjectMeta: metav1.ObjectMeta{Name: "bar", DeletionTimestamp: now()},
		mutator:    &fakeMutator{},
	}
	c.objects[key(m)] = orObject(m)

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key(m)})
	if !errors.Is(err, errSome) {
		t.Errorf("got Reconcile() error = %v, want %v", err, errSome)
	}
}
