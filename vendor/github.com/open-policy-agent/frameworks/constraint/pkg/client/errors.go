package client

import (
	"errors"
)

var (
	ErrCreatingBackend              = errors.New("unable to create backend")
	ErrNoDriverName                 = errors.New("driver has no name")
	ErrNoReferentialDriver          = errors.New("no driver that supports referential constraints added")
	ErrDuplicateDriver              = errors.New("duplicate drivers of the same name")
	ErrCreatingClient               = errors.New("unable to create client")
	ErrMissingConstraint            = errors.New("missing Constraint")
	ErrMissingConstraintTemplate    = errors.New("missing ConstraintTemplate")
	ErrInvalidModule                = errors.New("invalid module")
	ErrReview                       = errors.New("target.HandleReview failed")
	ErrUnsupportedEnforcementPoints = errors.New("enforcement point not supported by client")
)

// IsUnrecognizedConstraintError returns true if err is an ErrMissingConstraint.
//
// Deprecated: Use errors.Is(err, ErrMissingConstraint) instead.
func IsUnrecognizedConstraintError(err error) bool {
	return errors.Is(err, ErrMissingConstraint)
}
