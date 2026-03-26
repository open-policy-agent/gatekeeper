package client

import (
	"errors"
)

// Client error variables.
var (
	// ErrCreatingBackend indicates a failure to create a backend.
	ErrCreatingBackend = errors.New("unable to create backend")
	// ErrNoDriverName indicates a driver was provided without a name.
	ErrNoDriverName = errors.New("driver has no name")
	// ErrNoReferentialDriver indicates no referential driver was configured.
	ErrNoReferentialDriver = errors.New("no driver that supports referential constraints added")
	// ErrDuplicateDriver indicates multiple drivers with the same name were added.
	ErrDuplicateDriver = errors.New("duplicate drivers of the same name")
	// ErrCreatingClient indicates a failure to create a client.
	ErrCreatingClient = errors.New("unable to create client")
	// ErrMissingConstraint indicates a required constraint is missing.
	ErrMissingConstraint = errors.New("missing Constraint")
	// ErrMissingConstraintTemplate indicates a required constraint template is missing.
	ErrMissingConstraintTemplate = errors.New("missing ConstraintTemplate")
	// ErrInvalidModule indicates an invalid Rego module.
	ErrInvalidModule = errors.New("invalid module")
	// ErrReview indicates a failure during target review handling.
	ErrReview = errors.New("target.HandleReview failed")
	// ErrUnsupportedEnforcementPoints indicates unsupported enforcement points.
	ErrUnsupportedEnforcementPoints = errors.New("enforcement point not supported by client")
)

// IsUnrecognizedConstraintError returns true if err is an ErrMissingConstraint.
//
// Deprecated: Use errors.Is(err, ErrMissingConstraint) instead.
func IsUnrecognizedConstraintError(err error) bool {
	return errors.Is(err, ErrMissingConstraint)
}
