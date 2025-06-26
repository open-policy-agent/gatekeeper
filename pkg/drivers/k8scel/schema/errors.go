package schema

import "errors"

var (
	ErrBadMatchCondition    = errors.New("invalid match condition")
	ErrBadVariable          = errors.New("invalid variable definition")
	ErrBadFailurePolicy     = errors.New("invalid failure policy")
	ErrBadResourceOperation = errors.New("invalid resource operation")
	ErrCELEngineMissing     = errors.New("K8sNativeValidation engine is missing")
	ErrOneTargetAllowed     = errors.New("wrong number of targets defined, only 1 target allowed")
	ErrBadType              = errors.New("could not recognize the type")
)
