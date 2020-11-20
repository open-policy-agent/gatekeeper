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

package token

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanner(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
	}{
		{
			input: `foo.bar.baz`,
			expected: []Token{
				{Type: IDENT, Literal: "foo"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "bar"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `	  foo   .  bar	.	baz  	   `,
			expected: []Token{
				{Type: IDENT, Literal: "foo"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "bar"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: ` "foo bar"   . . baz  	   `,
			expected: []Token{
				{Type: IDENT, Literal: "foo bar"},
				{Type: SEPARATOR, Literal: "."},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: "\n we\t\r\n.\nsupport\t    \n[newlines\t\n:\n*]\n",
			expected: []Token{
				{Type: IDENT, Literal: "we"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "support"},
				{Type: LBRACKET, Literal: "["},
				{Type: IDENT, Literal: "newlines"},
				{Type: COLON, Literal: ":"},
				{Type: GLOB, Literal: "*"},
				{Type: RBRACKET, Literal: "]"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `        	   `,
			expected: []Token{
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `0123_foobar_baz`,
			expected: []Token{
				{Type: IDENT, Literal: "0123_foobar_baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `  .0123_foobar_baz  .`,
			expected: []Token{
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "0123_foobar_baz"},
				{Type: SEPARATOR, Literal: "."},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `0123_foobar-baz`,
			expected: []Token{
				{Type: IDENT, Literal: "0123_foobar-baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `-valid-identifier-`,
			expected: []Token{
				{Type: IDENT, Literal: `-valid-identifier-`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `'...'."..."`,
			expected: []Token{
				{Type: IDENT, Literal: `...`},
				{Type: SEPARATOR, Literal: `.`},
				{Type: IDENT, Literal: `...`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `..''`,
			expected: []Token{
				{Type: SEPARATOR, Literal: `.`},
				{Type: SEPARATOR, Literal: `.`},
				{Type: IDENT, Literal: ``},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `spec."this object"."is very"["much full": 'of everyone\'s'].'favorite thing'`,
			expected: []Token{
				{Type: IDENT, Literal: "spec"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "this object"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "is very"},
				{Type: LBRACKET, Literal: "["},
				{Type: IDENT, Literal: "much full"},
				{Type: COLON, Literal: ":"},
				{Type: IDENT, Literal: "of everyone's"},
				{Type: RBRACKET, Literal: "]"},
				{Type: SEPARATOR, Literal: "."},
				{Type: IDENT, Literal: "favorite thing"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `"won't \"confuse\" the [scanner: *nope*]"`,
			expected: []Token{
				{Type: IDENT, Literal: `won't "confuse" the [scanner: *nope*]`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `'won\'t "confuse" the [scanner: *nope*]'`,
			expected: []Token{
				{Type: IDENT, Literal: `won't "confuse" the [scanner: *nope*]`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `][**:**][`,
			expected: []Token{
				{Type: RBRACKET, Literal: `]`},
				{Type: LBRACKET, Literal: `[`},
				{Type: GLOB, Literal: `*`},
				{Type: GLOB, Literal: `*`},
				{Type: COLON, Literal: `:`},
				{Type: GLOB, Literal: `*`},
				{Type: GLOB, Literal: `*`},
				{Type: RBRACKET, Literal: `]`},
				{Type: LBRACKET, Literal: `[`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `"foo" "bar"`,
			expected: []Token{
				{Type: IDENT, Literal: `foo`},
				{Type: IDENT, Literal: `bar`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `foo bar`,
			expected: []Token{
				{Type: IDENT, Literal: `foo`},
				{Type: IDENT, Literal: `bar`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `"unterminated string '`,
			expected: []Token{
				{Type: ERROR, Literal: `unterminated string '`},
			},
		},
		{
			input: `"also unterminated\"`,
			expected: []Token{
				{Type: ERROR, Literal: `also unterminated"`},
			},
		},
		{
			input: `"🤔☕️❗️"`,
			expected: []Token{
				{Type: IDENT, Literal: `🤔☕️❗️`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `"🦋bb\🐥aa🦄"`,
			expected: []Token{
				{Type: IDENT, Literal: `🦋bb🐥aa🦄`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `Moo🐙`,
			expected: []Token{
				{Type: IDENT, Literal: `Moo`},
				{Type: ERROR, Literal: `🐙`},
			},
		},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			s := NewScanner(tc.input)
			var tokens []Token
			for {
				tok := s.Next()
				tokens = append(tokens, tok)
				if tok.Type == EOF || tok.Type == ERROR {
					break
				}
			}

			diff := cmp.Diff(tc.expected, tokens)
			if diff != "" {
				t.Errorf("for input: %s\nunexpected tokens: %s", tc.input, diff)
			}
		})
	}
}

func TestScanner_EOF(t *testing.T) {
	s := NewScanner("")
	expected := Token{Type: EOF, Literal: ""}
	for i := 0; i < 5; i++ {
		if tok := s.Next(); tok != expected {
			t.Errorf("[%d]: unexpected token: %s", i, tok.String())
		}
	}
}
