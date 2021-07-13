package gktest

// Filter filters tests and cases to run.
type Filter struct{}

// NewFilter parses run into a Filter for selecting constraint tests and
// individual cases to run.
//
// Empty string results in a Filter which matches all tests and their cases.
//
// Examples:
// 1) NewFiler("require-foo-label//missing-label")
// Matches tests containing the string "require-foo-label"
// and cases containing the string "missing-label". So this would match all of the
// following:
// - Test: "require-foo-label", Case: "missing-label"
// - Test: "not-require-foo-label, Case: "not-missing-label"
// - Test: "require-foo-label-and-bar-annotation", Case: "missing-label-and-annotation"
//
// 2) NewFilter("missing-label")
// Matches tests which either have a name containing "missing-label" or which
// are in a constraint test containing "missing-label". Matches the following:
// - Test: "forbid-missing-label", Case: "with-foo-label"
// - Test: "required-labels", Case: "missing-label"
//
// 3) NewFilter("^require-foo-label$//")
// Matches tests in constraints which exactly match "require-foo-label". Matches the
// following:
// - Test: "require-foo-label", Case: "with-foo-label"
// - Test: "require-foo-label", Case: "no-labels"
//
// 4) NewFilter("//empty-object")
// Matches tests whose names contain the string "empty-object". Matches the
// following:
// - Test: "forbid-foo-label", Case: "empty-object"
// - Test: "forbid-foo-label", Case: "another-empty-object"
// - Test: "require-bar-annotation", Case: "empty-object".
func NewFilter(run string) (Filter, error) {
	return Filter{}, nil
}

// MatchesTest filters the set of constraint tests to run by constraint name
// and the tests contained in the constraint. Returns true if tests in the constraint
// should be run.
//
// If a constraint regex was specified, returns true if the constraint regex
// matches `constraint`.
// If a constraint regex was not specified but a test regex was, returns true if
// at least one test in `tests` matches the test regex.
func (f Filter) MatchesTest(c Test) bool {
	return true
}

// MatchesCase filters the set of tests to run by name.
//
// Returns true if the test regex matches test.
func (f Filter) MatchesCase(t Case) bool {
	return true
}
