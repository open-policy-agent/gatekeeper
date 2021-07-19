package gktest

import (
	"fmt"
	"regexp"
)

// Filter filters tests and cases to run.
type Filter struct {
	regex *regexp.Regexp
}

// NewFilter parses run into a Filter for selecting constraint tests and
// individual cases to run.
//
// Empty string results in a Filter which matches all tests and their cases.
//
// Examples:
// 1) NewFiler("require-foo-label//missing-label")
// Matches tests ending with the string "require-foo-label"
// and cases beginning with the string "missing-label". So this would match all
// of the following:
// - Test: "require-foo-label", Case: "missing-label"
// - Test: "not-require-foo-label, Case: "not-missing-label"
// - Test: "require-foo-label", Case: "missing-label-and-annotation"
//
// 2) NewFilter("missing-label")
// Matches cases which either have a name containing "missing-label" or which
// are in a test named "missing-label". Matches the following:
// - Test: "forbid-missing-label", Case: "with-foo-label"
// - Test: "required-labels", Case: "missing-label"
//
// 3) NewFilter("^require-foo-label//")
// Matches tests which exactly match "require-foo-label". Matches the following:
// - Test: "require-foo-label", Case: "with-foo-label"
// - Test: "require-foo-label", Case: "no-labels"
//
// 4) NewFilter("//empty-object")
// Matches tests whose names contain the string "empty-object". Matches the
// following:
// - Test: "forbid-foo-label", Case: "empty-object"
// - Test: "forbid-foo-label", Case: "another-empty-object"
// - Test: "require-bar-annotation", Case: "empty-object".
func NewFilter(filter string) (*Filter, error) {
	if filter == "" {
		return &Filter{}, nil
	}

	regex, err := regexp.Compile(filter)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
	}

	return &Filter{regex: regex}, nil
}

// MatchesTest filters the set of tests to run by test name
// and the cases contained in the test. Returns true if the Test should be run.
//
// If a constraint regex was specified, returns true if the constraint regex
// matches `constraint`.
// If a constraint regex was not specified but a test regex was, returns true if
// at least one test in `tests` matches the test regex.
func (f Filter) MatchesTest(t Test) bool {
	if f.regex == nil {
		return true
	}

	testName := t.Name + "//"
	if f.regex.MatchString(testName) {
		return true
	}

	for _, c := range t.Cases {
		if f.MatchesCase(t.Name, c.Name) {
			return true
		}
	}

	return false
}

// MatchesCase filters Cases to run by name.
//
// Returns true if the test regex matches test.
func (f Filter) MatchesCase(testName, caseName string) bool {
	if f.regex == nil {
		return true
	}

	fullName := fmt.Sprintf("%s//%s", testName, caseName)

	return f.regex.MatchString(fullName)
}
