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
	"strings"
	"unicode/utf8"
)

const eof = rune(-1)

// Base errors for scanning strings.
var (
	ErrUnterminatedString = errors.New("unterminated string")
	ErrInvalidCharacter   = errors.New("invalid character")
)

type Scanner struct {
	input   string
	pos     int // Current position
	readPos int // Next position to read
	ch      rune
	err     error // Last error if any
}

func NewScanner(input string) *Scanner {
	s := &Scanner{input: input}
	s.read()
	return s
}

func (s *Scanner) Next() Token {
	var err error
	tok := Token{Type: ERROR}
	s.skipWhitespace()

	switch {
	// A match on these first set of cases leaves s.ch positioned at the next character to process.
	case isDigit(s.ch):
		if tok.Literal, err = s.readInt(); err == nil {
			tok.Type = INT
		}
	case isAlphaNum(s.ch):
		if tok.Literal, err = s.readIdent(); err == nil {
			tok.Type = IDENT
		}

	default:
		// Any of these cases require a subsequent call to s.read() (below) to position the next character.
		switch s.ch {
		case eof:
			tok = Token{Type: EOF, Literal: ""}
		case '.':
			tok = Token{Type: SEPARATOR, Literal: string(s.ch)}
		case '[':
			tok = Token{Type: LBRACKET, Literal: string(s.ch)}
		case ']':
			tok = Token{Type: RBRACKET, Literal: string(s.ch)}
		case '*':
			tok = Token{Type: GLOB, Literal: string(s.ch)}
		case ':':
			tok = Token{Type: COLON, Literal: string(s.ch)}
		case '"', '\'':
			if tok.Literal, err = s.readString(); err == nil {
				tok.Type = IDENT
			}
		default:
			// default: current character is invalid at this location
			s.setError(ErrInvalidCharacter)
			tok = Token{Type: ERROR, Literal: string(s.ch)}
		}

		// Make progress
		s.read()
	}

	return tok
}

// read consumes the next rune and advances.
func (s *Scanner) read() rune {
	if s.readPos >= len(s.input) {
		s.ch = eof
		s.pos = len(s.input)
		return eof
	}
	r, w := utf8.DecodeRuneInString(s.input[s.readPos:])
	s.pos = s.readPos // Mark last read position
	s.readPos += w    // Advance for next read
	s.ch = r
	return r
}

// readString consumes a string token.
func (s *Scanner) readString() (string, error) {
	quote := s.ch // Will be ' or "
	var out strings.Builder

	for {
		s.read()
		switch {
		case s.ch == quote:
			// String terminated
			return out.String(), nil
		case s.ch == '\\':
			// Escaped character
			s.read()
			if s.ch == eof {
				continue
			}
			out.WriteRune(s.ch)
		case s.ch == eof:
			// Unterminated string
			s.setError(ErrUnterminatedString)
			return out.String(), s.err

		default:
			out.WriteRune(s.ch)
		}
	}
}

func (s *Scanner) readIdent() (string, error) {
	start := s.pos
	for isAlphaNum(s.ch) {
		s.read()
	}
	return s.input[start:s.pos], s.err
}

// readInt scans a (positive) integer. Signs are not supported.
func (s *Scanner) readInt() (string, error) {
	start := s.pos
	for isDigit(s.ch) {
		s.read()
	}
	return s.input[start:s.pos], s.err
}

func (s *Scanner) setError(err error) {
	s.err = ScanError{
		Inner:    err,
		Position: s.pos,
	}
}

// isSpace returns true if the passed rune is a supported whitespace character.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

func isAlphaNum(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z':
	case 'A' <= r && r <= 'Z':
	case '0' <= r && r <= '9':
	case r == '_':
	case r == '-':

	default:
		return false
	}
	return true
}

func isDigit(r rune) bool {
	return '0' <= r && r <= '9'
}

func (s *Scanner) skipWhitespace() {
	for isSpace(s.ch) {
		s.read()
	}
}

type ScanError struct {
	Inner    error
	Position int
}

func (e ScanError) Error() string {
	var innerMsg string
	if e.Inner != nil {
		innerMsg = e.Inner.Error()
	}
	return fmt.Sprintf("error at position %d: %s", e.Position, innerMsg)
}

// Unwrap allows errors.Is() to inspect the underlying error.
func (e ScanError) Unwrap() error {
	return e.Inner
}
