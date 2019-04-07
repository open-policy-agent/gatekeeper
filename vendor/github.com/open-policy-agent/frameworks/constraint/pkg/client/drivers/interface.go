package drivers

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

type Driver interface {
	Init(ctx context.Context) error

	PutModule(ctx context.Context, name string, src string) error
	DeleteModule(ctx context.Context, name string) (bool, error)

	PutData(ctx context.Context, path string, data interface{}) error
	DeleteData(ctx context.Context, path string) (bool, error)

	Query(ctx context.Context, path string, input interface{}) (*types.Response, error)

	Dump(ctx context.Context) (string, error)
}
