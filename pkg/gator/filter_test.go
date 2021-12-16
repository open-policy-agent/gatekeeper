package gator

import (
	"errors"
	"testing"
)

func TestFilter_Error(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		filter string
		want   error
	}{
		{
			name:   "empty filter",
			filter: "",
			want:   nil,
		},
		{
			name:   "or filter",
			filter: "labels",
			want:   nil,
		},
		{
			name:   "or filter error",
			filter: "labels(",
			want:   ErrInvalidFilter,
		},
		{
			name:   "and filter",
			filter: "labels//allow",
			want:   nil,
		},
		{
			name:   "and filter test error",
			filter: "labels(//allow",
			want:   ErrInvalidFilter,
		},
		{
			name:   "and filter case error",
			filter: "labels//allow(",
			want:   ErrInvalidFilter,
		},
		{
			name:   "too many splits error",
			filter: "a//b//c",
			want:   ErrInvalidFilter,
		},
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewFilter(tc.filter)
			if !errors.Is(err, tc.want) {
				t.Fatalf(`got NewFilter("(") error = %v, want %v`, err, ErrInvalidFilter)
			}
		})
	}
}

func TestFilter_MatchesTest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		filter string
		test   Test
		want   bool
	}{
		{
			name:   "empty matches test",
			filter: "",
			test:   Test{Name: "foo"},
			want:   true,
		},
		{
			name:   "filter matches test with no cases",
			filter: "foo",
			test:   Test{Name: "foo"},
			want:   true,
		},
		{
			name:   "filter matches test with no cases submatch",
			filter: "foo",
			test:   Test{Name: "foo-bar"},
			want:   true,
		},
		{
			name:   "filter matches test with no cases 2",
			filter: "foo//",
			test:   Test{Name: "foo"},
			want:   true,
		},
		{
			name:   "filter matches case",
			filter: "foo",
			test:   Test{Name: "bar", Cases: []*Case{{Name: "foo"}}},
			want:   true,
		},
		{
			name:   "filter matches case submatch",
			filter: "foo",
			test:   Test{Name: "bar", Cases: []*Case{{Name: "foo-bar"}}},
			want:   true,
		},
		{
			name:   "test name matches test with no cases 2",
			filter: "foo//",
			test:   Test{Name: "foo"},
			want:   true,
		},
		{
			name:   "test name mismatch",
			filter: "foo//",
			test:   Test{Name: "bar", Cases: []*Case{{Name: "foo"}}},
			want:   false,
		},
		{
			name:   "test and case match",
			filter: "bar//qux",
			test:   Test{Name: "bar", Cases: []*Case{{Name: "qux"}}},
			want:   true,
		},
		{
			name:   "test and case submatch",
			filter: "bar//qux",
			test:   Test{Name: "foo-bar", Cases: []*Case{{Name: "qux-corge"}}},
			want:   true,
		},
		{
			name:   "test mismatch",
			filter: "bar-bar//qux",
			test:   Test{Name: "bar-foo", Cases: []*Case{{Name: "qux-corge"}}},
			want:   false,
		},
		{
			name:   "case mismatch",
			filter: "bar//qux-qux",
			test:   Test{Name: "foo", Cases: []*Case{{Name: "corge-qux"}}},
			want:   false,
		},
		{
			name:   "test match case mismatch",
			filter: "bar-bar//qux-qux",
			test:   Test{Name: "bar", Cases: []*Case{{Name: "foo"}}},
			want:   false,
		},
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter, err := NewFilter(tc.filter)
			if err != nil {
				t.Fatal(err)
			}

			got := filter.MatchesTest(tc.test)
			if got != tc.want {
				t.Errorf("got MatchesTest(%q) = %t, want %t", tc.test.Name, got, tc.want)
			}
		})
	}
}

func TestFilter_MatchesCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		filter    string
		wantMatch map[string]map[string]bool
	}{
		{
			name:   "empty matches all cases",
			filter: "",
			wantMatch: map[string]map[string]bool{
				"bar": {
					"foo": true,
					"qux": true,
				},
				"corge": {
					"foo": true,
					"qux": true,
				},
			},
		},
		{
			name:   "test name match",
			filter: "bar//",
			wantMatch: map[string]map[string]bool{
				"bar": {
					"foo": true,
					"qux": true,
				},
				"corge": {
					"foo": false,
					"qux": false,
				},
			},
		},
		{
			name:   "case name match",
			filter: "//foo",
			wantMatch: map[string]map[string]bool{
				"bar": {
					"foo": true,
					"qux": false,
				},
				"corge": {
					"foo": true,
					"qux": false,
				},
			},
		},
		{
			name:   "test and case name match",
			filter: "bar//foo",
			wantMatch: map[string]map[string]bool{
				"bar": {
					"foo": true,
					"qux": false,
				},
				"corge": {
					"foo": false,
					"qux": false,
				},
			},
		},
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter, err := NewFilter(tc.filter)
			if err != nil {
				t.Fatal(err)
			}

			for testName, cases := range tc.wantMatch {
				for caseName, want := range cases {
					got := filter.MatchesCase(testName, caseName)

					if got != want {
						t.Errorf("got Filter(%q).MatchesCase(%q, %q) = %t, want %t",
							tc.filter, testName, caseName, got, want)
					}
				}
			}
		})
	}
}
