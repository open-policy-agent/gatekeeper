package gator

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Client interface {
	// AddTemplate adds a Template to the Client. Templates define the structure
	// and parameters of potential Constraints.
	AddTemplate(ctx context.Context, templ *templates.ConstraintTemplate) (*types.Responses, error)

	// AddConstraint adds a Constraint to the Client. Must map to one of the
	// previously-added Templates.
	//
	// Returns an error if the referenced Template does not exist, or the
	// Constraint does not match the structure defined by the referenced Template.
	AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) (*types.Responses, error)

	// AddData adds the state of the cluster. For use in referential Constraints.
	AddData(ctx context.Context, data interface{}) (*types.Responses, error)

	// RemoveData removes objects from the state of the cluster. For use in
	// referential constraints.
	RemoveData(ctx context.Context, data interface{}) (*types.Responses, error)

	// Review runs all Constraints against obj.
	Review(ctx context.Context, obj interface{}, opts ...reviews.ReviewOpt) (*types.Responses, error)
}
