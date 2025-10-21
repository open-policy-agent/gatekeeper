package transform

import "errors"

var (
	ErrBadEnforcementAction = errors.New("invalid enforcement action")
	ErrOperationMismatch    = errors.New("operations mismatch between webhook and constraint template")
)
