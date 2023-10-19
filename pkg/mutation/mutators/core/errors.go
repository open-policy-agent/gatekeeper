package core

import "errors"

// ErrNonKeyedSetter occurs when a setter that doesn't understand keyed lists
// is called against a keyed list.
var (
	ErrNonKeyedSetter = errors.New("mutator does not understand keyed lists")
	ErrNameLength     = errors.New("maximum length for name is 63 char")
)
