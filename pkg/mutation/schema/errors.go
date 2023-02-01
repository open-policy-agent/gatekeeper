package schema

import (
	"errors"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
)

// ErrNilMutator reports that a method which expected an actual Mutator was
// a nil pointer.
const ErrNilMutator = util.Error("attempted to add nil mutator")

func NewErrConflictingSchema(ids IDSet) error {
	return ErrConflictingSchema{Conflicts: ids}
}

const ErrConflictingSchemaType = "ErrConflictingSchema"

// ErrConflictingSchema reports that adding a Mutator to the DB resulted in
// conflicting implicit schemas.
type ErrConflictingSchema struct {
	Conflicts IDSet
}

func (e ErrConflictingSchema) Error() string {
	return fmt.Sprintf("the following mutators have conflicting schemas: %v",
		e.Conflicts.String())
}

func (e ErrConflictingSchema) Is(other error) bool {
	var o ErrConflictingSchema
	if !errors.As(other, &o) {
		return false
	}

	if len(e.Conflicts) != len(o.Conflicts) {
		return false
	}

	for id := range e.Conflicts {
		if !o.Conflicts[id] {
			return false
		}
	}

	return true
}
