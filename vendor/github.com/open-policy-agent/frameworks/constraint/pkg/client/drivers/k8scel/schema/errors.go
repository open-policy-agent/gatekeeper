package schema

import "errors"

var (
	ErrBadMatchCondition = errors.New("invalid match condition")
	ErrBadVariable       = errors.New("invalid variable definition")
	ErrBadFailurePolicy  = errors.New("invalid failure policy")
	ErrCodeNotDefined    = errors.New("K8sNativeValidation code not defined")
	ErrOneTargetAllowed  = errors.New("wrong number of targets defined, only 1 target allowed")
	ErrBadType           = errors.New("Could not recognize the type")
	ErrMissingField      = errors.New("K8sNativeValidation source missing required field")
)
