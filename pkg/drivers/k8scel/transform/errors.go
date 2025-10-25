package transform

import "errors"

var (
	ErrBadEnforcementAction = errors.New("invalid enforcement action")
	ErrOperationMismatch    = errors.New("operations mismatch between webhook and constraint template")
	ErrOperationNoMatch     = errors.New("no matching operations between webhook and constraint template")
)
