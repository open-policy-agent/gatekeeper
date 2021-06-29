package gktest

// Filter filters suites and tests to run.
type Filter struct{}

// NewFilter parses run into a Filter for selecting suites and individual tests
// to run.
//
// Empty string results in a Filter which matches all tests.
//
// Examples:
// 1) NewFiler("require-foo-label//missing-label")
// Matches tests in suites containing the string "require-foo-label" and tests
// containing the string "missing-label". So this would match all of the
// following:
// - Suite: "require-foo-label", Test: "missing-label"
// - Suite: "not-require-foo-label, Test: "not-missing-label"
// - Suite: "require-foo-label-and-bar-annotation", Test: "missing-label-and-annotation"
//
// 2) NewFilter("missing-label")
// Matches tests which either have a name containing "missing-label" or which
// are in a suite containing "missing-label". Matches the following:
// - Suite: "forbid-missing-label", Test: "with-foo-label"
// - Suite: "required-labels", Test: "missing-label"
//
// 3) NewFilter("^require-foo-label$//")
// Matches tests in suites which exactly match "require-foo-label". Matches the
// following:
// - Suite: "require-foo-label", Test: "with-foo-label"
// - Suite: "require-foo-label", Test: "no-labels"
//
// 4) NewFilter("//empty-object")
// Matches tests whose names contain the string "empty-object". Matches the
// following:
// - Suite: "forbid-foo-label", Test: "empty-object"
// - Suite: "forbid-foo-label", Test: "another-empty-object"
// - Suite: "require-bar-annotation", Test: "empty-object"
func NewFilter(run string) (Filter, error) {
	return Filter{}, nil
}

// MatchesSuite filters the set of test suites to run by suite name and the tests
// contained in the suite. Returns true if the suite should be run.
//
// If a suite regex was specified, returns true if the suite regex matches
// `suite`.
// If a suite regex was not specified but a test regex was, returns true if at
// least one test in `tests` matches the test regex.
func (f Filter) MatchesSuite(suite Suite) bool {
	return true
}

// MatchesTest filters the set of tests to run by name.
//
// Returns true if the test regex matches test.
func (f Filter) MatchesTest(testCase TestCase) bool {
	return true
}
