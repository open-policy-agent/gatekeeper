package errors

import "errors"

var (
	ErrAutoreject     = errors.New("unable to match constraints")
	ErrModuleName     = errors.New("invalid module name")
	ErrParse          = errors.New("unable to parse module")
	ErrCompile        = errors.New("unable to compile modules")
	ErrModulePrefix   = errors.New("invalid module prefix")
	ErrPathInvalid    = errors.New("invalid data path")
	ErrPathConflict   = errors.New("conflicting path")
	ErrWrite          = errors.New("error writing data")
	ErrRead           = errors.New("error reading data")
	ErrTransaction    = errors.New("error committing data")
	ErrCreatingDriver = errors.New("error creating Driver")

	ErrInvalidConstraintTemplate = errors.New("invalid ConstraintTemplate")
	ErrMissingConstraintTemplate = errors.New("missing ConstraintTemplate")
	ErrInvalidModule             = errors.New("invalid module")
	ErrChangeTargets             = errors.New("ConstraintTemplates with Constraints may not change targets")
)
