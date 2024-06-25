package drivers

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/opa/storage"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A Driver implements Rego query execution of Templates and Constraints.
type Driver interface {
	// Name returns the name of the driver, used to uniquely identify a driver
	// and in errors returned to the user.
	Name() string

	// AddTemplate compiles a Template's code to be specified by
	// Constraints and referenced in Query. Replaces the existing Template if it
	// already exists.
	AddTemplate(ctx context.Context, ct *templates.ConstraintTemplate) error
	// RemoveTemplate removes the Template from the Driver, and any Constraints.
	// Does not return an error if the Template does not exist.
	RemoveTemplate(ctx context.Context, ct *templates.ConstraintTemplate) error

	// AddConstraint adds a Constraint to Driver for a particular Template. Future
	// calls to Query may reference the added Constraint. Replaces the existing
	// Constraint if it already exists.
	AddConstraint(ctx context.Context, constraint *unstructured.Unstructured) error
	// RemoveConstraint removes a Constraint from Driver. Future calls to Query
	// may not reference the removed Constraint.
	// Does not return error if the Constraint does not exist.
	RemoveConstraint(ctx context.Context, constraint *unstructured.Unstructured) error

	// AddData caches data to be used for referential Constraints. Replaces data
	// if it already exists at the specified path. This is a deprecated method that
	// will only be called for the "Rego" driver.
	AddData(ctx context.Context, target string, path storage.Path, data interface{}) error
	// RemoveData removes cached data, so the data at the specified path can no
	// longer be used in referential Constraints. This is a deprecated method that
	// will only be called for the "Rego" driver.
	RemoveData(ctx context.Context, target string, path storage.Path) error

	// Query runs the passed target's Constraints against review.
	// Returns a QueryResponse type.
	// Returns an error if there was a problem executing the Query.
	Query(ctx context.Context, target string, constraints []*unstructured.Unstructured, review interface{}, opts ...reviews.ReviewOpt) (*QueryResponse, error)

	// Dump outputs the entire state of compiled Templates, added Constraints, and
	// cached data used for referential Constraints.
	Dump(ctx context.Context) (string, error)

	// GetDescriptionForStat returns the description for a given stat name
	// or errors out for an unknown stat.
	GetDescriptionForStat(statName string) (string, error)
}

// ConstraintKey uniquely identifies a Constraint.
type ConstraintKey struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// ConstraintKeyFrom returns a unique identifier corresponding to Constraint.
func ConstraintKeyFrom(constraint *unstructured.Unstructured) ConstraintKey {
	return ConstraintKey{
		Kind: constraint.GetKind(),
		Name: constraint.GetName(),
	}
}

// StoragePath returns a unique path in Rego storage for Constraint's parameters.
// Constraints have a single set of parameters shared among all targets, so a
// target-specific path is not required.
func (k ConstraintKey) StoragePath() storage.Path {
	return storage.Path{"constraints", k.Kind, k.Name}
}
