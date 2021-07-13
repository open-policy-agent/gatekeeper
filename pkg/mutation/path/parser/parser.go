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

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/token"
)

type parser struct {
	input     string
	scanner   *token.Scanner
	curToken  token.Token
	peekToken token.Token
	err       error
}

// Parse parses the provided input and returns an abstract representation if successful.
func Parse(input string) (Path, error) {
	p := newParser(input)
	return p.Parse()
}

func newParser(input string) *parser {
	s := token.NewScanner(input)
	p := &parser{
		input:   input,
		scanner: s,
	}
	p.curToken = p.scanner.Next()
	p.peekToken = p.scanner.Next()
	return p
}

// next advances to the next token in the stream.
func (p *parser) next() {
	p.curToken = p.peekToken
	p.peekToken = p.scanner.Next()
}

// expect returns whether the next token matches our expectation,
// and if so advances to that token.
// Otherwise returns false and doesn't advance.
func (p *parser) expect(t token.Type) bool {
	if p.peekToken.Type == t {
		p.next()
		return true
	}
	return false
}

// expectPeek returns whether the next token matches our expectation.
// The current token is not advanced either way.
func (p *parser) expectPeek(t token.Type) bool {
	return p.peekToken.Type == t
}

func (p *parser) Parse() (Path, error) {
	root := Path{}
	for p.curToken.Type == token.IDENT && p.err == nil {
		if node := p.parseObject(); node != nil {
			root.Nodes = append(root.Nodes, node)
		}

		// Check for optional listSpec operator
		if p.expect(token.LBRACKET) {
			if node := p.parseList(); node != nil {
				root.Nodes = append(root.Nodes, node)
			}
		}

		// Advance past separator if needed and ensure no unexpected tokens follow.
		// NOTE: expect() advances the current position if the next token is a match.
		switch {
		case p.expect(token.SEPARATOR):
			if p.expectPeek(token.EOF) {
				// block trailing separators
				p.setError(ErrTrailingSeparator)
				return Path{}, p.err
			}
			// Skip past the separator
			p.next()
		case p.expect(token.EOF):
			// Allowed. Loop will exit.
		default:
			p.setError(fmt.Errorf("%w: expected '.' or eof, got: %s", ErrUnexpectedToken, p.peekToken.String()))
			return Path{}, p.err
		}
	}

	if p.curToken.Type != token.EOF {
		p.setError(fmt.Errorf("%w: expected field name or eof, got: %s", ErrUnexpectedToken, p.curToken.String()))
	}
	if p.err != nil {
		return Path{}, p.err
	}

	return root, nil
}

// parseList tries to parse the current position as List match node, e.g. [key: val]
// returns nil if it cannot be parsed as a List.
func (p *parser) parseList() Node {
	out := &List{}

	// keyField is required
	if !p.expect(token.IDENT) {
		p.setError(fmt.Errorf("%w: expected keyField in listSpec, got: %s", ErrUnexpectedToken, p.peekToken.String()))
		return nil
	}

	out.KeyField = p.curToken.Literal

	if !p.expect(token.COLON) {
		p.setError(fmt.Errorf("%w: expected ':' following keyField %s, got: %s", ErrUnexpectedToken, out.KeyField, p.peekToken.String()))
		return nil
	}

	switch {
	case p.expect(token.GLOB):
		out.Glob = true
	case p.expect(token.IDENT):
		out.KeyValue = p.curToken.Literal
	case p.expect(token.INT):
		val, err := parseInt64(p.curToken.Literal)
		if err != nil {
			p.setError(fmt.Errorf("%w: parsing key value for key: %s", err, out.KeyField))
			return nil
		}
		out.KeyValue = val
	default:
		p.setError(fmt.Errorf("%w: expected key value or glob in listSpec, got: %s", ErrUnexpectedToken, p.peekToken.String()))
		return nil
	}

	if !p.expect(token.RBRACKET) {
		p.setError(fmt.Errorf("%w: expected ']' following listSpec, got: %s", ErrUnexpectedToken, p.peekToken.String()))
		return nil
	}
	return out
}

func (p *parser) parseObject() Node {
	out := &Object{Reference: p.curToken.Literal}
	return out
}

func (p *parser) setError(err error) {
	// Support only the first error for now
	if p.err != nil {
		return
	}
	p.err = err
}

// parseInt64 will return the int64 representation of the decimal encoded in the string s.
// This function was written because strconv.ParseInt() parses octal and hexadecimal representations
// which we are not supporting in our syntax.
func parseInt64(s string) (int64, error) {
	var result int64
	for _, d := range s {
		if d < '0' || d > '9' {
			return 0, invalidIntegerError{s: fmt.Sprintf("unexpected digit: %c", d)}
		}
		result = result*10 + int64(d-'0')
		if result < 0 {
			return 0, invalidIntegerError{s: fmt.Sprintf("overflow in integer string: %s", s)}
		}
	}
	return result, nil
}
