package clients

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NoopClient struct{}

func (f *NoopClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	return nil
}

func (f *NoopClient) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	return nil
}

func (f *NoopClient) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	return nil
}

func (f *NoopClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	return nil
}

func (f *NoopClient) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return nil
}

func (f *NoopClient) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	return nil
}

func (f *NoopClient) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	return nil
}

func (f *NoopClient) Status() client.StatusWriter {
	return f
}
