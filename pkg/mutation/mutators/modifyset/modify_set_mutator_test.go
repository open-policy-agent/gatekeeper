package modifyset

import (
	"testing"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/testhelpers"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_ModifySet_errors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mut    *mutationsunversioned.ModifySet
		errMsg string
	}{
		{
			name:   "empty spec",
			mut:    &mutationsunversioned.ModifySet{},
			errMsg: "applyTo required for ModifySet mutator",
		},
		{
			name: "name > 63",
			mut: &mutationsunversioned.ModifySet{
				ObjectMeta: v1.ObjectMeta{
					Name: testhelpers.BigName(),
				},
			},
			errMsg: core.ErrNameLength.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator, err := MutatorForModifySet(tt.mut)

			require.ErrorContains(t, err, tt.errMsg)
			require.Nil(t, mutator)
		})
	}
}
