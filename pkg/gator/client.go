package gator

import (
	"context"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Client interface {
	// AddTemplate adds a Template to the Client. Templates define the structure
	// and parameters of potential Constraints.
	AddTemplate(templ *templates.ConstraintTemplate) (*types.Responses, error)

	// AddConstraint adds a Constraint to the Client. Must map to one of the
	// previously-added Templates.
	//
	// Returns an error if the referenced Template does not exist, or the
	// Constraint does not match the structure defined by the referenced Template.
	AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error)

	// AddCachedData adds the state of the cluster. For use in referential Constraints.
	AddCachedData(ctx context.Context, data interface{}) (*types.Responses, error)

	// RemoveCachedData removes objects from the state of the cluster. For use in
	// referential constraints.
	RemoveCachedData(ctx context.Context, data interface{}) (*types.Responses, error)

	// Review runs all Constraints against obj.
	Review(ctx context.Context, obj interface{}, opts ...constraintclient.QueryOpt) (*types.Responses, error)

	// Audit makes sure the cached state of the system satisfies all stored constraints.
	// On error, the responses return value will still be populated so that
	// partial results can be analyzed.
	Audit(ctx context.Context, opts ...constraintclient.QueryOpt) (*types.Responses, error)
}
