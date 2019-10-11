package regorewriter

import (
	"testing"

	"github.com/open-policy-agent/opa/ast"
)

func TestPackagePrefixer(t *testing.T) {
	testcases := []struct {
		name    string
		prefix  string
		ref     string
		wantRef string
	}{
		{
			name:    "only data",
			prefix:  "x.y.z",
			ref:     "data",
			wantRef: "data.x.y.z",
		},
		{
			name:    "data with stuff",
			prefix:  "x.y.z",
			ref:     "data.foo",
			wantRef: "data.x.y.z.foo",
		},
		{
			name:    "data with more stuff",
			prefix:  "x.y.z",
			ref:     "data.foo.bar",
			wantRef: "data.x.y.z.foo.bar",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pp := NewPackagePrefixer(tc.prefix)

			ref := ast.MustParseRef(tc.ref)
			wantRef := ast.MustParseRef(tc.wantRef)

			gotRef := pp.Transform(ref)
			if !wantRef.Equal(gotRef) {
				t.Errorf("wanted %s, got %s", wantRef, gotRef)
			}

		})
	}
}
