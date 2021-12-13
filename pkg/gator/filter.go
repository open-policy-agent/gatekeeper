package gator

import (
	"fmt"
	"regexp"
	"strings"
)

type Filter interface {
	// MatchesTest returns true if the Test should be run.
	MatchesTest(Test) bool
	// MatchesCase returns true if Case caseName in Test testName should be run.
	MatchesCase(testName, caseName string) bool
}

// NewFilter parses run into a Filter for selecting constraint tests and
// individual cases to run.
//
// Empty string results in a Filter which matches all tests and their cases.
//
// Examples:
// 1) NewFiler("require-foo-label//missing-label")
// Matches tests containing the string "require-foo-label"
// and cases containing the string "missing-label". So this would match all
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
// 3) NewFilter("^require-foo-label$//")
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
func NewFilter(filter string) (Filter, error) {
	if filter == "" {
		return &nilFilter{}, nil
	}

	filters := strings.Split(filter, "//")

	switch len(filters) {
	case 1:
		return newOrFilter(filters[0])
	case 2:
		return newAndFilter(filters[0], filters[1])
	default:
		return nil, fmt.Errorf(`%w: a filter may include at most one "//"`, ErrInvalidFilter)
	}
}

// nilFilter matches all tests.
type nilFilter struct{}

var _ Filter = &nilFilter{}

func (f *nilFilter) MatchesTest(Test) bool {
	return true
}

func (f *nilFilter) MatchesCase(string, string) bool {
	return true
}

// orFilter matches:
// 1) Tests which are matched by regex.
// 2) Tests which contain a Case matched by regex.
// 3) Cases which are matched by regex.
type orFilter struct {
	regex *regexp.Regexp
}

func newOrFilter(filter string) (Filter, error) {
	regex, err := regexp.Compile(filter)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
	}

	return &orFilter{regex: regex}, nil
}

func (f *orFilter) MatchesTest(t Test) bool {
	if f.regex.MatchString(t.Name) {
		return true
	}

	for _, c := range t.Cases {
		if f.MatchesCase(t.Name, c.Name) {
			return true
		}
	}

	return false
}

func (f *orFilter) MatchesCase(testName, caseName string) bool {
	return f.regex.MatchString(caseName) || f.regex.MatchString(testName)
}

// andFilter matches Cases which match caseRegex which are in Tests which match
// testRegex.
type andFilter struct {
	testRegex *regexp.Regexp
	caseRegex *regexp.Regexp
}

var _ Filter = &andFilter{}

func newAndFilter(testFilter, caseFilter string) (Filter, error) {
	testRegex, err := regexp.Compile(testFilter)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
	}

	caseRegex, err := regexp.Compile(caseFilter)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
	}

	return &andFilter{testRegex: testRegex, caseRegex: caseRegex}, nil
}

func (f *andFilter) MatchesTest(t Test) bool {
	return f.testRegex.MatchString(t.Name)
}

func (f *andFilter) MatchesCase(testName, caseName string) bool {
	return f.caseRegex.MatchString(caseName) && f.testRegex.MatchString(testName)
}
