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
	// ErrNumViolations indicates an Object did not get the expected number of
	// violations.
	ErrNumViolations = errors.New("unexpected number of violations")
	// ErrInvalidRegex indicates a Case specified a Violation regex that could not
	// be compiled.
	ErrInvalidRegex = errors.New("message contains invalid regular expression")
	// ErrInvalidFilter indicates that Filter construction failed.
	ErrInvalidFilter = errors.New("invalid test filter")
	// ErrNoObjects indicates that a test Case's object file has no YAML documents
	ErrNoObjects = errors.New("missing objects")
)
