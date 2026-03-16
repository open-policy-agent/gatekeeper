// Package errors defines error types for constraint operations.
//
//nolint:revive // Package name intentionally conflicts with stdlib; use alias "clienterrors" when importing.
package errors

import "errors"

var (
	// ErrAutoreject is returned when constraints cannot be matched.
	ErrAutoreject = errors.New("unable to match constraints")
	// ErrModuleName is returned when a module name is invalid.
	ErrModuleName = errors.New("invalid module name")
	// ErrParse is returned when a module cannot be parsed.
	ErrParse = errors.New("unable to parse module")
	// ErrCompile is returned when modules cannot be compiled.
	ErrCompile = errors.New("unable to compile modules")
	// ErrModulePrefix is returned when a module prefix is invalid.
	ErrModulePrefix = errors.New("invalid module prefix")
	// ErrPathInvalid is returned when a data path is invalid.
	ErrPathInvalid = errors.New("invalid data path")
	// ErrPathConflict is returned when there is a conflicting path.
	ErrPathConflict = errors.New("conflicting path")
	// ErrWrite is returned when there is an error writing data.
	ErrWrite = errors.New("error writing data")
	// ErrRead is returned when there is an error reading data.
	ErrRead = errors.New("error reading data")
	// ErrTransaction is returned when there is an error committing data.
	ErrTransaction = errors.New("error committing data")
	// ErrCreatingDriver is returned when there is an error creating a Driver.
	ErrCreatingDriver = errors.New("error creating Driver")

	// ErrInvalidConstraintTemplate is returned when a ConstraintTemplate is invalid.
	ErrInvalidConstraintTemplate = errors.New("invalid ConstraintTemplate")
	// ErrMissingConstraintTemplate is returned when a ConstraintTemplate is missing.
	ErrMissingConstraintTemplate = errors.New("missing ConstraintTemplate")
	// ErrInvalidModule is returned when a module is invalid.
	ErrInvalidModule = errors.New("invalid module")
	// ErrChangeTargets is returned when attempting to change targets on a ConstraintTemplate with Constraints.
	ErrChangeTargets = errors.New("ConstraintTemplates with Constraints may not change targets")
	// ErrNoDriver is returned when no language driver handles the constraint template.
	ErrNoDriver = errors.New("no language driver is installed that handles this constraint template")
)
