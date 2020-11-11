package mutation

import (
	"sync"
)

// System keeps the list of mutations and
// provides an interface to apply mutations.
type System struct {
	// schemaDB schema.DB commented to please the linter, will uncomment in the implementation
	mutators []Mutator
	sync.RWMutex
}

// NewSystem initializes an empty mutation system
func NewSystem() *System {
	return &System{
		mutators: make([]Mutator, 0),
	}
}
