package modifyset

import (
	"testing"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/stretchr/testify/require"
)

// Tests the ModifySet mutator MutatorForModifySet call with an empty spec for graceful handling.
func Test_ModifySet_emptySpec(t *testing.T) {
	modifySet := &mutationsunversioned.ModifySet{}
	mutator, err := MutatorForModifySet(modifySet)

	require.ErrorContains(t, err, "applyTo required for ModifySet mutator")
	require.Nil(t, mutator)
}
