package drivers

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

	// AddTemplate adds the template source code to OPA
	AddTemplate(ct *templates.ConstraintTemplate) error
	// RemoveTemplate removes the template source code from OPA
	RemoveTemplate(ct *templates.ConstraintTemplate) error
	// AddConstraint inserts validated constraint into OPA
	AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) error
	// RemoveConstraint removes a constraint from OPA
	RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) error
	PutData(ctx context.Context, path string, data interface{}) error
	DeleteData(ctx context.Context, path string) (bool, error)
	Query(ctx context.Context, path string, input interface{}, opts ...QueryOpt) (*types.Response, error)
	Dump(ctx context.Context) (string, error)
}
