package mutators

import (
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
)

// MutatorForAssign returns an AssignMutator built from
// the given assign instance.
func MutatorForAssign(a *mutationsv1alpha1.Assign) (*assign.Mutator, error) {
	return assign.MutatorForAssign(a)
}

// MutatorForAssignMetadata builds an AssignMetadataMutator from the given AssignMetadata object.
func MutatorForAssignMetadata(assignMeta *mutationsv1alpha1.AssignMetadata) (*assignmeta.Mutator, error) {
	return assignmeta.MutatorForAssignMetadata(assignMeta)
}

// MutatorForModifySet builds an AssignMetadataMutator from the given ModifySet object.
func MutatorForModifySet(modifySet *mutationsv1alpha1.ModifySet) (*modifyset.Mutator, error) {
	return modifyset.MutatorForModifySet(modifySet)
}
