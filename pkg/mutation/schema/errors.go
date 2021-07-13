package schema

import "github.com/open-policy-agent/gatekeeper/pkg/util"

// ErrNilMutator reports that a method which expected an actual Mutator was
// a nil pointer.
const ErrNilMutator = util.Error("attempted to add nil mutator")

// ErrConflictingSchema reports that adding a Mutator to the DB resulted in
// conflicting implicit schemas.
const ErrConflictingSchema = util.Error("mutator schema conflict")
