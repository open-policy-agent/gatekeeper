package core

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fakeClient struct {
	client.Client

	objects map[types.NamespacedName]client.Object
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		objects: make(map[types.NamespacedName]client.Object),
	}
}

type fakeMutator struct {
	client.Object

	Namespace string
	Name string
}

func (m *fakeMutator) GetNamespace() string {
	return m.Namespace
}

func (m *fakeMutator) GetName() string {
	return m.Name
}

func (m *fakeMutator) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: m.GetNamespace(), Name: m.GetName()}
}

type fakeLogger struct {
	logr.Logger
}

func TestReconciler_Reconcile_Insert(t *testing.T) {
	testCases := []struct {
		name string
		mutators []*fakeMutator
	}{
		{
			name: "one mutator",
			mutators: []*fakeMutator{{
				Namespace: "foo",
				Name: "bar",
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient()
			r := &Reconciler{
				Client: c,
				log: fakeLogger{},
			}

			ctx := context.Background()

			for _, m := range tc.mutators {
				c.objects[m.GetNamespacedName()] = m

				_, _ = r.Reconcile(ctx, reconcile.Request{
					NamespacedName: m.GetNamespacedName(),
				})
			}

		})
	}
}
