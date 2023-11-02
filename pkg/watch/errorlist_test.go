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
				er.AddGVKErr(gvkErr.gvk, gvkErr.err)
			}
			for _, err := range tc.errsToAdd.errs {
				er.Add(err)
			}

			require.ElementsMatch(t, tc.expectedGVKs, er.FailingGVKs())
			require.Equal(t, tc.generalErr, er.HasGeneralErr())
		})
	}
}
