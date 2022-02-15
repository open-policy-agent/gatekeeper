package errors

import "errors"

var (
	ErrModuleName   = errors.New("invalid module name")
	ErrParse        = errors.New("unable to parse module")
	ErrCompile      = errors.New("unable to compile modules")
	ErrModulePrefix = errors.New("invalid module prefix")
	ErrPathInvalid  = errors.New("invalid data path")
	ErrPathConflict = errors.New("conflicting path")
	ErrWrite        = errors.New("error writing data")
	ErrRead         = errors.New("error reading data")
	ErrTransaction  = errors.New("error committing data")

	ErrInvalidConstraintTemplate = errors.New("invalid ConstraintTemplate")
	ErrInvalidConstraint         = errors.New("invalid Constraint")
	ErrMissingConstraintTemplate = errors.New("missing ConstraintTemplate")
	ErrInvalidModule             = errors.New("invalid module")
)
