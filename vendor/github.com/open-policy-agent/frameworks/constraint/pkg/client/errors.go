package client

import (
	"errors"
)

var (
	ErrCreatingBackend           = errors.New("unable to create backend")
	ErrCreatingClient            = errors.New("unable to create client")
	ErrMissingConstraint         = errors.New("missing Constraint")
	ErrMissingConstraintTemplate = errors.New("missing ConstraintTemplate")
	ErrInvalidModule             = errors.New("invalid module")
	ErrReview                    = errors.New("target.HandleReview failed")
)

// IsUnrecognizedConstraintError returns true if err is an ErrMissingConstraint.
//
// Deprecated: Use errors.Is(err, ErrMissingConstraint) instead.
func IsUnrecognizedConstraintError(err error) bool {
	return errors.Is(err, ErrMissingConstraint)
}
