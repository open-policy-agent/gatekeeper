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
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanner(t *testing.T) {
	tests := []struct {
		input    string
		expected []Token
		wantErr  error
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
			input: `"one"two`,
			expected: []Token{
				{Type: IDENT, Literal: "one"},
				{Type: IDENT, Literal: "two"},
				{Type: EOF, Literal: ""},
			},
		},
		// Integer token support
		{
			input: `0123`,
			expected: []Token{
				{Type: INT, Literal: "0123"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `9876543210`,
			expected: []Token{
				{Type: INT, Literal: "9876543210"},
				{Type: EOF, Literal: ""},
			},
		},
		// Tokenizer doesn't care about bit length
		{
			input: `99918446744073709551615`,
			expected: []Token{
				{Type: INT, Literal: "99918446744073709551615"},
				{Type: EOF, Literal: ""},
			},
		},
		// Hexadecimal not supported
		{
			input: `0x1A`,
			expected: []Token{
				{Type: INT, Literal: "0"},
				{Type: IDENT, Literal: "x1A"},
				{Type: EOF, Literal: ""},
			},
		},
		// Signs not supported
		{
			input: `-1024`,
			expected: []Token{
				{Type: IDENT, Literal: "-1024"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `+1024`,
			expected: []Token{
				{Type: ERROR, Literal: "+"},
			},
			wantErr: ErrInvalidCharacter,
		},
		// Decimals are not supported
		{
			input: `3.14`,
			expected: []Token{
				{Type: INT, Literal: "3"},
				{Type: SEPARATOR, Literal: "."},
				{Type: INT, Literal: "14"},
				{Type: EOF, Literal: ""},
			},
		},
		// Identifiers can no longer lead with digits
		{
			input: `0123_foobar_baz`,
			expected: []Token{
				{Type: INT, Literal: "0123"},
				{Type: IDENT, Literal: "_foobar_baz"},
				{Type: EOF, Literal: ""},
			},
		},
		// Quoted numbers are identifiers
		{
			input: `"0123_foobar_baz"`,
			expected: []Token{
				{Type: IDENT, Literal: "0123_foobar_baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `"299792458"`,
			expected: []Token{
				{Type: IDENT, Literal: "299792458"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `  .0123_foobar_baz  .`,
			expected: []Token{
				{Type: SEPARATOR, Literal: "."},
				{Type: INT, Literal: "0123"},
				{Type: IDENT, Literal: "_foobar_baz"},
				{Type: SEPARATOR, Literal: "."},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `0123_foobar-baz`,
			expected: []Token{
				{Type: INT, Literal: "0123"},
				{Type: IDENT, Literal: "_foobar-baz"},
				{Type: EOF, Literal: ""},
			},
		},
		// Digits can be part of an unquoted identifier, just not at the start.
		{
			input: `Nexus6`,
			expected: []Token{
				{Type: IDENT, Literal: "Nexus6"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			input: `ItWasTheSummerOf69'🎸'`,
			expected: []Token{
				{Type: IDENT, Literal: "ItWasTheSummerOf69"},
				{Type: IDENT, Literal: "🎸"},
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
			wantErr: ErrUnterminatedString,
		},
		{
			input: `"also unterminated\"`,
			expected: []Token{
				{Type: ERROR, Literal: `also unterminated"`},
			},
			wantErr: ErrUnterminatedString,
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
			wantErr: ErrInvalidCharacter,
		},
		{
			input: "\"quoted\nnewline\"",
			expected: []Token{
				{Type: IDENT, Literal: "quoted\nnewline"},
				{Type: EOF, Literal: ""},
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

			if !errors.Is(s.err, tc.wantErr) {
				t.Errorf("for input: %s\ngot scanner error %v, want %v", tc.input, s.err, tc.wantErr)
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
