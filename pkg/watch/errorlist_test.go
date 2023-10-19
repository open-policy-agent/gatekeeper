package watch

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Test_WatchesError(t *testing.T) {
	someErr := errors.New("some err")
	someGVKA := schema.GroupVersionKind{Group: "a", Version: "b", Kind: "c"}
	someGVKB := schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"}

	type errsToAdd struct {
		errs    []error
		gvkErrs []gvkErr
	}

	for _, tt := range []struct {
		name         string
		errsToAdd    errsToAdd
		expectedGVKs []schema.GroupVersionKind
		generalErr   bool
	}{
		{
			name: "gvk errors, no global",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
				},
			},
			expectedGVKs: []schema.GroupVersionKind{someGVKA, someGVKB},
			generalErr:   false,
		},
		{
			name: "gvk errors and global",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
				},
				errs: []error{someErr, someErr},
			},
			expectedGVKs: []schema.GroupVersionKind{someGVKA, someGVKB},
			generalErr:   true,
		},
		{
			name: "just global",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{},
				errs:    []error{someErr},
			},
			generalErr: true,
		},
		{
			name: "nothing",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			er := errorList{}
			for _, gvkErr := range tt.errsToAdd.gvkErrs {
				er.AddGVKErr(gvkErr.gvk, gvkErr.err)
			}
			for _, err := range tt.errsToAdd.errs {
				er.Add(err)
			}

			require.ElementsMatch(t, tt.expectedGVKs, er.FailingGVKs())
			require.Equal(t, tt.generalErr, er.HasGeneralErr())
		})
	}
}
