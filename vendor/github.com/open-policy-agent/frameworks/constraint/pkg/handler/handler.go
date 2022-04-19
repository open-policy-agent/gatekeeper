package handler

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/crds"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type TargetHandler interface {
	crds.MatchSchemaProvider

	// GetName returns name of the target. Must match `^[a-zA-Z][a-zA-Z0-9.]*$`
	// This will be the exact name of the field in the ConstraintTemplate
	// spec.target object, so if GetName returns validation.xyz.org, the user
	// will populate target specific rego into .spec.targets."validation.xyz.org".
	GetName() string

	// ProcessData takes inputs to AddData and converts them into the format that
	// will be stored in data.inventory and returns the relative storage path.
	// Args:
	//	data: the object passed to client.Client.AddData
	// Returns:
	//	handle: true if the target handles the data type
	//	key: the unique relative path under which the data should be stored in OPA
	//	under data.inventory, for example, an item to be stored at
	//	data.inventory.x.y.z would return []string{"x", "y", "z"}
	//	inventoryFormat: the data as an object that can be cast into JSON and suitable for storage in the inventory
	//	err: any error encountered
	ProcessData(data interface{}) (handle bool, key []string, inventoryFormat interface{}, err error)

	// HandleReview determines if this target handler will handle an individual
	// resource review and if so, builds the `review` field of the input object.
	// Args:
	//	object: the object passed to client.Client.Review
	// Returns:
	//	handle: true if the target handler will review this input
	//	review: the data for the `review` field
	//	err: any error encountered.
	HandleReview(object interface{}) (handle bool, review interface{}, err error)

	// ValidateConstraint returns an error if constraint is not valid in any way.
	// This allows for semantic validation beyond OpenAPI validation given by the
	// spec from MatchSchema().
	ValidateConstraint(constraint *unstructured.Unstructured) error

	// ToMatcher converts a Constraint to its corresponding Matcher.
	// Allows caching Constraint-specific logic for matching objects under
	// review.
	ToMatcher(constraint *unstructured.Unstructured) (constraints.Matcher, error)
}
