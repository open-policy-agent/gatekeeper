package gktest

import "errors"

var (
	// ErrNotATemplate indicates the user-indicated file does not contain a
	// ConstraintTemplate.
	ErrNotATemplate = errors.New("not a ConstraintTemplate")
	// ErrNotAConstraint indicates the user-indicated file does not contain a
	// Constraint.
	ErrNotAConstraint = errors.New("not a Constraint")
	// ErrAddingTemplate indicates a problem instantiating a Suite's ConstraintTemplate.
	ErrAddingTemplate = errors.New("adding template")
	// ErrAddingConstraint indicates a problem instantiating a Suite's Constraint.
	ErrAddingConstraint = errors.New("adding constraint")
	// ErrInvalidSuite indicates a Suite does not define the required fields.
	ErrInvalidSuite = errors.New("invalid Suite")
	// ErrCreatingClient indicates an error instantiating the Client which compiles
	// Constraints and runs validation.
	ErrCreatingClient = errors.New("creating client")
	// ErrInvalidCase indicates a Case cannot be run due to not being configured properly.
	ErrInvalidCase = errors.New("invalid Case")
	// ErrUnexpectedNoViolations indicates a Case failed because it was expected
	// to get violations matching an Assertion, but did not get any.
	ErrUnexpectedNoViolations = errors.New("got no violations")
	// ErrUnexpectedViolation indicates a Case failed because it got unexpected
	// violations matching an Assertion.
	ErrUnexpectedViolation = errors.New("got violations")
	// ErrNumViolations indicates an Object did not get the expected number of
	// violations.
	ErrNumViolations = errors.New("unexpected number of violations")
	// ErrInvalidRegex indicates a Case specified a Violation regex that could not
	// be compiled.
	ErrInvalidRegex = errors.New("message contains invalid regular expression")
)
