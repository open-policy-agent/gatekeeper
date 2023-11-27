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
	someGVKD := schema.GroupVersionKind{Group: "p", Version: "q", Kind: "r"}

	type errsToAdd struct {
		errs    []error
		gvkErrs []gvkErr
	}

	tcs := []struct {
		name               string
		errsToAdd          errsToAdd
		expectedAddGVKs    []schema.GroupVersionKind
		expectedRemoveGVKs []schema.GroupVersionKind
		expectGeneralErr   bool
	}{
		{
			name: "gvk errors, not general",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
					{err: someErr, gvk: someGVKD, isRemove: true},
				},
			},
			expectedAddGVKs:    []schema.GroupVersionKind{someGVKA, someGVKB},
			expectedRemoveGVKs: []schema.GroupVersionKind{someGVKD},
		},
		{
			name: "gvk errors and general error",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
					{err: someErr, gvk: someGVKC, isRemove: true},
				},
				errs: []error{someErr, someErr},
			},
			expectedAddGVKs:    []schema.GroupVersionKind{someGVKA, someGVKB},
			expectedRemoveGVKs: []schema.GroupVersionKind{someGVKC},
			expectGeneralErr:   true,
		},
		{
			name: "just general error",
			errsToAdd: errsToAdd{
				errs: []error{someErr},
			},
			expectGeneralErr: true,
		},
		{
			name: "just add gvk error",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKA},
					{err: someErr, gvk: someGVKB},
				},
			},
			expectedAddGVKs: []schema.GroupVersionKind{someGVKA, someGVKB},
		},
		{
			name: "just remove gvk error",
			errsToAdd: errsToAdd{
				gvkErrs: []gvkErr{
					{err: someErr, gvk: someGVKC, isRemove: true},
					{err: someErr, gvk: someGVKD, isRemove: true},
				},
			},
			expectedRemoveGVKs: []schema.GroupVersionKind{someGVKC, someGVKD},
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

			require.ElementsMatch(t, tc.expectedAddGVKs, er.AddGVKFailures())
			require.ElementsMatch(t, tc.expectedRemoveGVKs, er.RemoveGVKFailures())
			require.Equal(t, tc.expectGeneralErr, er.HasGeneralErr())
		})
	}
}
