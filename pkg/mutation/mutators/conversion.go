package mutators

import (
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignimage"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
)

// MutatorForAssign returns an AssignMutator built from
// the given assign instance.
func MutatorForAssign(a *mutationsunversioned.Assign) (*assign.Mutator, error) {
	return assign.MutatorForAssign(a)
}

// MutatorForAssignMetadata builds an AssignMetadataMutator from the given AssignMetadata object.
func MutatorForAssignMetadata(assignMeta *mutationsunversioned.AssignMetadata) (*assignmeta.Mutator, error) {
	return assignmeta.MutatorForAssignMetadata(assignMeta)
}

// MutatorForModifySet builds a ModifySetMutator from the given ModifySet object.
func MutatorForModifySet(modifySet *mutationsunversioned.ModifySet) (*modifyset.Mutator, error) {
	return modifyset.MutatorForModifySet(modifySet)
}

// MutatorForAssignImage builds an AssignImageMutator from the given AssignImage
// object.
func MutatorForAssignImage(assignImage *mutationsunversioned.AssignImage) (*assignimage.Mutator, error) {
	return assignimage.MutatorForAssignImage(assignImage)
}
