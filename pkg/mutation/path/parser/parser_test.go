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
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParser(t *testing.T) {
	tests := []struct {
		input    string
		expected []Node
		wantErr  error
	}{
		{
			// empty returns empty
			input:    ``,
			expected: nil,
		},
		{
			// we don't allow a leading separator
			input:    `.spec`,
			expected: nil,
			wantErr:  ErrUnexpectedToken,
		},
		{
			// we don't allow a trailing separator
			input:    `spec.`,
			expected: nil,
			wantErr:  ErrTrailingSeparator,
		},
		{
			// we allow escaped quotes in identifiers
			input: `"sp\"ec"`,
			expected: []Node{
				&Object{Reference: "sp\"ec"},
			},
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
				&List{KeyField: "name", KeyValue: "*", Glob: false},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers[name: foo].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: "foo"},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers["my key": "foo bar"]`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "my key", KeyValue: "foo bar"},
			},
		},
		{
			// Error: keys with whitespace must be quoted
			input:   `spec.containers[my key: "foo bar"]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// Error: values with whitespace must be quoted
			input:   `spec.containers[key: foo bar]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input: `spec.containers[name: ""].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: ""},
				&Object{Reference: "securityContext"},
			},
		},
		{
			input: `spec.containers["": "someValue"].securityContext`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "", KeyValue: "someValue"},
				&Object{Reference: "securityContext"},
			},
		},
		{
			// Parsing error: either glob or field value are required in listSpec
			input:   `spec.containers[name: ].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// parse error: listSpec requires keyField
			input:   `spec.containers[].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `spec.containers[:].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `spec.containers[:foo].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `spec.containers[foo].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `spec.containers[*].securityContext`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// parse error: we don't allow empty segments
			input:   `foo..bar`,
			wantErr: ErrUnexpectedToken,
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
			input:   `[foo: bar]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// List cannot follow list
			input:   `[foo: bar][bar: *]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// List cannot follow valid list
			input:   `spec.containers[foo: bar][bar: *]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// List cannot follow separator
			input:   `spec.[foo: bar]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// Integer keyValues
			input: `spec.containers[name: opa].ports[containerPort: 8888].name`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "containers"},
				&List{KeyField: "name", KeyValue: "opa"},
				&Object{Reference: "ports"},
				&List{KeyField: "containerPort", KeyValue: int64(8888)},
				&Object{Reference: "name"},
			},
		},
		{
			// Integer keyFields not supported
			input:   `spec.containers[123: opa]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// Maximum 64bit integer
			input: `spec[bignum: 9223372036854775807]`,
			expected: []Node{
				&Object{Reference: "spec"},
				&List{KeyField: "bignum", KeyValue: int64(9223372036854775807)},
			},
		},
		{
			// Integer overflow
			input:   `spec[bignum: 9223372036854775808]`,
			wantErr: ErrInvalidInteger,
		},
		{
			// Quoted integers are parsed as strings
			input: `spec[quoted: "123"]`,
			expected: []Node{
				&Object{Reference: "spec"},
				&List{KeyField: "quoted", KeyValue: "123"},
			},
		},
		{
			// allow leading dash
			input: `-123-_456_`,
			expected: []Node{
				&Object{Reference: "-123-_456_"},
			},
		},
		{
			// allow trailing digits
			input: `area51`,
			expected: []Node{
				&Object{Reference: "area51"},
			},
		},
		{
			// field names cannot be integers
			input:   `012345`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `spec.123.bar`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// ...but they can be quoted strings that look like integers
			input: `"012345"`,
			expected: []Node{
				&Object{Reference: "012345"},
			},
		},
		{
			input: `spec."123"`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "123"},
			},
		},
		{
			input: `spec.studio54.bar`,
			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "studio54"},
				&Object{Reference: "bar"},
			},
		},
		{
			// Hexadecimal notation not supported
			input:   `spec[foo: 0x123]`,
			wantErr: ErrUnexpectedToken,
		},
		{
			// Octal notation not supported, interpreted as decimal.
			input: `spec[not_octal: 0123]`,
			expected: []Node{
				&Object{Reference: "spec"},
				&List{KeyField: "not_octal", KeyValue: int64(123)}, // rather than 83
			},
		},
		{
			// whitespace must be quoted
			input:   `spec.foo bar`,
			wantErr: ErrUnexpectedToken,
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
			input:   `*`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `][`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `foo[`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `[`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input:   `:`,
			wantErr: ErrUnexpectedToken,
		},
		{
			input: `spec."this object"."is very"["much full": 'of everyone\'s'].'favorite thing'`,

			expected: []Node{
				&Object{Reference: "spec"},
				&Object{Reference: "this object"},
				&Object{Reference: "is very"},
				&List{KeyField: "much full", KeyValue: "of everyone's"},
				&Object{Reference: "favorite thing"},
			},
		},
		{
			input: `"token-with-trailing-backslash\\"`,
			expected: []Node{
				&Object{Reference: `token-with-trailing-backslash\`},
			},
		},
		{
			input: `"token-with-\\embedded-backslash"`,
			expected: []Node{
				&Object{Reference: `token-with-\embedded-backslash`},
			},
		},
		// Verify round-tripping on strings-that-look-like-other-tokens
		{
			input: `'foo[bar: baz]'`,
			expected: []Node{
				&Object{Reference: `foo[bar: baz]`},
			},
		},
		{
			input: `'foo[bar:baz]'`,
			expected: []Node{
				&Object{Reference: `foo[bar:baz]`},
			},
		},
		{
			input: `'foo[bar:*]'`,
			expected: []Node{
				&Object{Reference: `foo[bar:*]`},
			},
		},
		{
			input: `'dot..dot'`,
			expected: []Node{
				&Object{Reference: `dot..dot`},
			},
		},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			root, err := Parse(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("for input: %s\ngot error: %v, want: %v", tc.input, err, tc.wantErr)
			}
			var nodes []Node
			if len(root.Nodes) != 0 {
				nodes = root.Nodes
			}
			diff := cmp.Diff(tc.expected, nodes)
			if diff != "" {
				t.Errorf("for input: %s\ngot unexpected results: %s", tc.input, diff)
			}

			// Ensure that converting a parsed Path into a String and back again
			// produces an identical Path.
			if len(root.Nodes) != 0 && tc.wantErr == nil {
				asString := root.String()

				reparsedRoot, err := Parse(asString)
				if err != nil {
					t.Fatalf("restringified %q into invalid path %q: %v", tc.input, asString, err)
				}

				if diff := cmp.Diff(tc.expected, reparsedRoot.Nodes); diff != "" {
					t.Errorf("unexpected difference with reparsed path: %s", diff)
				}
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
			input: &List{KeyField: "much full", KeyValue: "of everyone's"},
		},
		{
			name:  "test list deepcopy with nil nexted pointer",
			input: &List{KeyField: "much full", KeyValue: nil},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.input.DeepCopyNode()
			if diff := cmp.Diff(tc.input, out); diff != "" {
				t.Errorf("input and output differ: %s", diff)
			}
		})
	}
}
