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
				&Object{Value: "single_field"},
			},
		},
		{
			input: `spec.containers[name: *].securityContext`,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
				&List{KeyField: "name", Glob: true},
				&Object{Value: "securityContext"},
			},
		},
		{
			// A quoted '*' is a fieldValue, not a glob
			input: `spec.containers[name: "*"].securityContext`,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("*"), Glob: false},
				&Object{Value: "securityContext"},
			},
		},
		{
			input: `spec.containers[name: foo].securityContext`,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("foo")},
				&Object{Value: "securityContext"},
			},
		},
		{
			input: `spec.containers["my key": "foo bar"]`,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
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
				&Object{Value: "spec"},
				&Object{Value: "containers"},
				&List{KeyField: "name", KeyValue: strPtr("")},
				&Object{Value: "securityContext"},
			},
		},
		{
			// TODO: Is this useful or should we make it a parsing error?
			input: `spec.containers[name: ].securityContext`,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
				&List{KeyField: "name", KeyValue: nil},
				&Object{Value: "securityContext"},
			},
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
			// parse error: we don't allow empty segments
			input:     `foo..bar`,
			expectErr: true,
		},
		{
			// ...but we do allow zero-string-named segments
			input: `foo."".bar`,
			expected: []Node{
				&Object{Value: "foo"},
				&Object{Value: ""},
				&Object{Value: "bar"},
			},
		},
		{
			// whitespace can surround tokens
			input: `    spec   .    containers    `,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
			},
		},
		{
			// whitespace can surround tokens
			input: `    spec   .    "containers"    `,
			expected: []Node{
				&Object{Value: "spec"},
				&Object{Value: "containers"},
			},
		},
		{
			// TODO: Should this be allowed?
			input: `[foo: bar][bar: *]`,
			expected: []Node{
				&List{KeyField: "foo", KeyValue: strPtr("bar")},
				&List{KeyField: "bar", Glob: true},
			},
		},
		{
			// allow leading dash
			input: `-123-_456_`,
			expected: []Node{
				&Object{Value: "-123-_456_"},
			},
		},
		{
			// allow leading digits
			input: `012345`,
			expected: []Node{
				&Object{Value: "012345"},
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
				&Object{Value: "spec"},
				&Object{Value: "foo bar"},
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

func strPtr(s string) *string {
	return &s
}
