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
	someGVKC := schema.GroupVersionKind{Group: "m", Version: "n", Kind: "o"}

	type errsToAdd struct {
		errs    []error
		gvkErrs []gvkErr
	}

	tcs := []struct {
		name         string
		errsToAdd    errsToAdd
		expectedGVKs []schema.GroupVersionKind
		generalErr   bool
	}{
		{
			name: "gvk errors, not general",
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
			name: "gvk errors and general error",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
					{err: someErr, gvk: someGVKC, isRemove: true}, // this one should not show up in FailingGVKsToAdd
				},
				errs: []error{someErr, someErr},
			},
			expectedGVKs: []schema.GroupVersionKind{someGVKA, someGVKB},
			generalErr:   true,
		},
		{
			name: "just general error",
			errsToAdd: errsToAdd{
				errs: []error{someErr},
			},
			generalErr: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			er := NewErrorList()
			for _, gvkErr := range tc.errsToAdd.gvkErrs {
				if gvkErr.isRemove {
					er.RemoveGVKErr(gvkErr.gvk, gvkErr.err)
				} else {
					er.AddGVKErr(gvkErr.gvk, gvkErr.err)
				}
			}
			for _, err := range tc.errsToAdd.errs {
				er.Err(err)
			}

			require.ElementsMatch(t, tc.expectedGVKs, er.FailingGVKsToAdd())
			require.Equal(t, tc.generalErr, er.HasGeneralErr())
		})
	}
}
