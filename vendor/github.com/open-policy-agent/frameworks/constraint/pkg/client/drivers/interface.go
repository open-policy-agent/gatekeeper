package drivers

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

type QueryCfg struct {
	TracingEnabled bool
}

type QueryOpt func(*QueryCfg)

func Tracing(enabled bool) QueryOpt {
	return func(cfg *QueryCfg) {
		cfg.TracingEnabled = enabled
	}
}

type Driver interface {
	Init() error

	PutModule(name string, src string) error
	// PutModules upserts a number of modules under a given prefix.
	PutModules(namePrefix string, srcs []string) error

	// DeleteModules deletes all modules under a given prefix and returns the
	// count of modules deleted.  Deletion of non-existing prefix will
	// result in 0, nil being returned.
	DeleteModules(namePrefix string) (int, error)

	PutData(ctx context.Context, path string, data interface{}) error
	DeleteData(ctx context.Context, path string) (bool, error)

	Query(ctx context.Context, path string, input interface{}, opts ...QueryOpt) (*types.Response, error)

	Dump(ctx context.Context) (string, error)
}
