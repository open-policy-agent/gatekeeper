/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parser

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParser(t *testing.T) {
	tests := []struct {
		input     string
		expected  []Node
		expectErr bool
	}{
		{
			// empty returns empty
			input:    ``,
			expected: nil,
		},
		{
			// we don't allow a leading separator
			input:     `.spec`,
			expected:  nil,
			expectErr: true,
		},
		{
			// we don't allow a trailing separator
			input:     `spec.`,
			expected:  nil,
			expectErr: true,
		},
		{
			input: `single_field`,
			expected: []Node{
				&Object{Reference: "single_field"},
			},
		},
		{
			input: `spec.containers[name: *].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", Glob: true},
				&Object{Reference: "securityContext"},
			},
		},
		{
			// A quoted '*' is a fieldValue, not a glob
			input: `spec.containers[name: "*"].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("*"), Glob: false},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers[name: foo].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("foo")},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers["my key": "foo bar"]`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "my key", KeyValue: strPtr("foo bar")},
			},
		},
		{
			// Error: keys with whitespace must be quoted
			input:     `spec.containers[my key: "foo bar"]`,
			expectErr: true,
		},
		{
			// Error: values with whitespace must be quoted
			input:     `spec.containers[key: foo bar]`,
			expectErr: true,
		},
		{
			input: `spec.containers[name: ""].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("")},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers["": "someValue"].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "", KeyValue: strPtr("someValue")},
				&Object{Reference: "securityContext"},
			},
		},
		{
			// Parsing error: either glob or field value are required in listSpec
			input:     `spec.containers[name: ].securityContext`,
			expectErr: true,
		},
		{
			// parse error: listSpec requires keyField
			input:     `spec.containers[].securityContext`,
			expectErr: true,
		},
		{
			input:     `spec.containers[:].securityContext`,
			expectErr: true,
		},
		{
			input:     `spec.containers[:foo].securityContext`,
			expectErr: true,
		},
		{
			input:     `spec.containers[foo].securityContext`,
			expectErr: true,
		},
		{
			input:     `spec.containers[*].securityContext`,
			expectErr: true,
		},
		{
			// parse error: we don't allow empty segments
			input:     `foo..bar`,
			expectErr: true,
		},
		{
			// ...but we do allow zero-string-named segments
			input: `foo."".bar`,
			expected: []Node{
				&Object{Reference: "foo"},
				&Object{Reference: ""},
				&Object{Reference: "bar"},
			},
		},
		{
			// whitespace can surround tokens
			input: `    spec   .    containers    `,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
			},
		},
		{
			// whitespace can surround tokens
			input: `    spec   .    "containers"    `,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
			},
		},
		{
			// List cannot start the path
			input:     `[foo: bar]`,
			expectErr: true,
		},
		{
			// List cannot follow list
			input:     `[foo: bar][bar: *]`,
			expectErr: true,
		},
		{
			// List cannot follow valid list
			input:     `spec.containers[foo: bar][bar: *]`,
			expectErr: true,
		},
		{
			// List cannot follow separator
			input:     `spec.[foo: bar]`,
			expectErr: true,
		},
		{
			// allow leading dash
			input: `-123-_456_`,
			expected: []Node{
				&Object{Reference: "-123-_456_"},
			},
		},
		{
			// allow leading digits
			input: `012345`,
			expected: []Node{
				&Object{Reference: "012345"},
			},
		},
		{
			// whitespace must be quoted
			input:     `spec.foo bar`,
			expectErr: true,
		},
		{
			// whitespace must be quoted
			input: `spec."foo bar"`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "foo bar"},
			},
		},
		{
			// whitespace must be quoted
			input: "spec.\"foo\nbar\"",
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "foo\nbar"},
			},
		},
		{
			// unexpected tokens
			input:     `*`,
			expectErr: true,
		},
		{
			input:     `][`,
			expectErr: true,
		},
		{
			input:     `foo[`,
			expectErr: true,
		},
		{
			input:     `[`,
			expectErr: true,
		},
		{
			input:     `:`,
			expectErr: true,
		},
		{
			input: `spec."this object"."is very"["much full": 'of everyone\'s'].'favorite thing'`,

			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "this object"},
				&Object{Reference: "is very"},
				&List{KeyField: "much full", KeyValue: strPtr("of everyone's")},
				&Object{Reference: "favorite thing"},
			},
		},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			root, err := Parse(tc.input)
			if tc.expectErr != (err != nil) {
				t.Fatalf("for input: %s\nunexpected error: %v", tc.input, err)
			}
			var nodes []Node
			if root != nil {
				nodes = root.Nodes
			}
			diff := cmp.Diff(tc.expected, nodes)
			if diff != "" {
				t.Errorf("for input: %s\ngot unexpected results: %s", tc.input, diff)
			}
		})
	}

}

func TestDeepCopy(t *testing.T) {
	testCases := []struct {
		name  string
		input Node
	}{
		{
			name:  "test object deepcopy",
			input: &Object{Reference: "foo\nbar"},
		},
		{
			name:  "test list deepcopy",
			input: &List{KeyField: "much full", KeyValue: strPtr("of everyone's")},
		},
		{
			name:  "test list deepcopy with nil nexted pointer",
			input: &List{KeyField: "much full", KeyValue: nil},
		},
		{
			name: "test path deepcopy",
			input: &Path{
				Nodes: []Node{
					&List{KeyField: "much full", KeyValue: strPtr("of everyone's")},
					&List{KeyField: "name", KeyValue: strPtr("*"), Glob: false},
					&Object{Reference: "foo\nbar"},
					&Path{
						Nodes: []Node{
							&List{KeyField: "innername", KeyValue: strPtr("*"), Glob: false},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.input.DeepCopyNode()
			if !reflect.DeepEqual(tc.input, out) {
				t.Errorf("input and output differ, in: %v :: out %v", tc.input, out)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
