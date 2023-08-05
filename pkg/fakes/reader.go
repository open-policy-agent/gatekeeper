package fakes

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SpyReader struct {
	client.Reader
	ListFunc func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

func (r SpyReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.ListFunc != nil {
		return r.ListFunc(ctx, list, opts...)
	}
	return r.Reader.List(ctx, list, opts...)
}
