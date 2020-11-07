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

	"github.com/open-policy-agent/gatekeeper/pkg/path/token"
)

type Parser struct {
	input     string
	scanner   *token.Scanner
	curToken  token.Token
	peekToken token.Token
	err       error
}

func NewParser(input string) *Parser {
	s := token.NewScanner(input)
	p := &Parser{
		input:   input,
		scanner: s,
	}
	p.curToken = p.scanner.Next()
	p.peekToken = p.scanner.Next()
	return p
}

// next advances to the next token in the stream.
func (p *Parser) next() {
	p.curToken = p.peekToken
	p.peekToken = p.scanner.Next()
}

// expect returns whether the next token matches our expectation,
// and if so advances to that token.
// Otherwise returns false and doesn't advance.
func (p *Parser) expect(t token.Type) bool {
	if p.peekToken.Type == t {
		p.next()
		return true
	}
	return false
}

// expectPeek returns whether the next token matches our expectation.
// The current token is not advanced either way.
func (p *Parser) expectPeek(t token.Type) bool {
	return p.peekToken.Type == t
}

func (p *Parser) Parse() (*Root, error) {
	root := &Root{}
loop:
	for p.curToken.Type != token.EOF && p.err == nil {
		var node Node
		switch p.curToken.Type {
		case token.IDENT:
			node = p.parseObject()
		case token.LBRACKET:
			node = p.parseList()
		default:
			p.setError(fmt.Errorf("unexpected token: expected field name or eof, got: %s", p.peekToken.String()))
		}

		if p.err != nil {
			// Encountered parsing error, abort
			return nil, p.err
		}

		if node != nil {
			root.Nodes = append(root.Nodes, node)
		}

		// Advance past separator if needed and ensure no unexpected tokens follow
		switch {
		case p.expect(token.SEPARATOR):
			if p.expectPeek(token.EOF) {
				// block trailing separators
				p.setError(errors.New("trailing separators are forbidden"))
				return nil, p.err
			}
			// Skip past the separator
			p.next()
		case p.expect(token.LBRACKET):
			// Allowed but don't advance past the bracket
		case p.expect(token.EOF):
			break loop
		default:
			p.setError(fmt.Errorf("expected '.' or eof, got: %s", p.peekToken.String()))
			return nil, p.err
		}
	}
	return root, nil
}

// parseList tries to parse the current position as List match node, e.g. [key: val]
// returns nil if it cannot be parsed as a List.
func (p *Parser) parseList() Node {
	out := &List{}

	// keyField is required
	if !p.expect(token.IDENT) {
		p.setError(fmt.Errorf("expected keyField in listSpec, got: %s", p.peekToken.String()))
		return nil
	}

	out.KeyField = p.curToken.Literal

	if !p.expect(token.COLON) {
		p.setError(fmt.Errorf("expected ':' following keyField %s, got: %s", out.KeyField, p.peekToken.String()))
		return nil
	}

	switch {
	case p.expect(token.GLOB):
		out.Glob = true
	case p.expect(token.IDENT):
		// Optional
		val := p.curToken.Literal
		out.KeyValue = &val
	}

	if !p.expect(token.RBRACKET) {
		p.setError(fmt.Errorf("expected ']' following listSpec, got: %s", p.peekToken.String()))
		return nil
	}
	return out
}

func (p *Parser) parseObject() Node {
	out := &Object{Value: p.curToken.Literal}
	return out
}

func (p *Parser) setError(err error) {
	p.err = err
}
