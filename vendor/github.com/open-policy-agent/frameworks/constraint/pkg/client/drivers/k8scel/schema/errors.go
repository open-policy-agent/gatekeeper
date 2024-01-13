package schema

import "errors"

var (
	ErrBadMatchCondition = errors.New("invalid match condition")
	ErrBadVariable       = errors.New("invalid variable definition")
	ErrBadFailurePolicy  = errors.New("invalid failure policy")
)
